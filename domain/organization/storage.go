package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
)

// imagesDirectory is the structural directory under blobfs's root that
// holds every organization's image files. Ownership is at the file grain,
// the organization_image row, so the directory binds no organization.
const imagesDirectory = "organization-images"

// putLogo stores the upload as the organization's logo, named for its new
// file id with ext, and makes it the active one, retiring the logo it
// replaces. The file is written by the data package's write protocol, its
// pending row created after the organization is read, so a nonexistent
// organization is the missing row before any byte is stored. The pending
// row stands alone: no image references it until the write completes, so
// a write that stops partway, an aborted upload included, leaves a row the
// protocol abandons or blobfs's stale reclaim removes, and never an image
// that blocks the organization's delete.
//
// The completed file is then activated in one transaction: the file held,
// so no delete begins under the reference, the replaced logo's image
// removed and its delete begun, and the file's image inserted as the
// organization's active one. The replaced file is then purged, its object
// and its row. A concurrent replacement that activated first fails the
// insert with a unique violation; the new file is retired so it does not
// linger.
func (s *store) putLogo(ctx context.Context, organizationID string, u web.Upload, ext string) (Identity, error) {
	st := s.storage
	// On the pool, Ensure finds the directory a concurrent first upload
	// created between its lookup and its insert.
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory)
	if err != nil {
		return Identity{}, err
	}
	id := blobfs.NewID()
	file, err := st.Write(ctx, s.db.DB, u.Body, u.Size, func(tx *sqlate.Tx) (blobfs.File, error) {
		// A nonexistent organization is the missing row, before the
		// activation's foreign key would refuse it as a conflict.
		if _, err := s.view.One(ctx, tx, "id", organizationID); err != nil {
			return blobfs.File{}, err
		}
		return st.FS.Files.Create(ctx, tx, st.Objects, dir.ID, id+ext, u.ContentType, bfdata.WithID(id))
	})
	if err != nil {
		return Identity{}, err
	}
	replaced, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		if err := st.FS.Files.Hold(ctx, tx, file.ID); err != nil {
			return blobfs.File{}, err
		}
		current, err := s.activeLogo(ctx, tx, organizationID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return blobfs.File{}, err
		default:
			if current, err = s.release(ctx, tx, current.ID); err != nil {
				return blobfs.File{}, err
			}
		}
		return current, s.attach(ctx, tx, organizationID, file.ID)
	})
	if err != nil {
		// The activation rolled back, so no image references the new file.
		return Identity{}, errors.Join(err, st.Retire(ctx, s.db.DB, func(*sqlate.Tx) (string, error) { return file.ID, nil }))
	}
	if replaced.ID != "" {
		// The new logo is active and the replaced file unreferenced and
		// deleting, so a failed purge is the request's error though the
		// replacement stands; the stale reclaim finishes the file if no
		// retry does.
		if err := st.Purge(ctx, s.db.DB, replaced); err != nil {
			return Identity{}, fmt.Errorf("retire the replaced logo %s: %w", replaced.ID, err)
		}
	}
	return Identity{ID: file.ID, Version: file.Version}, nil
}

// logo reads the organization's active logo. Only an available file is
// served; any other is not found, as a missing logo is.
func (s *store) logo(ctx context.Context, organizationID string) (Logo, error) {
	file, err := s.activeLogo(ctx, s.db, organizationID)
	if err != nil {
		return Logo{}, err
	}
	obj, open, err := s.storage.Serve(ctx, file)
	if err != nil {
		return Logo{}, fmt.Errorf("logo of %s: %w", organizationID, err)
	}
	return Logo{Object: obj, Open: open}, nil
}

// deleteLogo retires the organization's active logo by the delete
// protocol, its image removed in the transaction that begins the delete;
// none is sql.ErrNoRows.
func (s *store) deleteLogo(ctx context.Context, organizationID string) error {
	return s.storage.Retire(ctx, s.db.DB, func(tx *sqlate.Tx) (string, error) {
		logo, err := s.activeLogo(ctx, tx, organizationID)
		if err != nil {
			return "", err
		}
		return logo.ID, s.detach(ctx, tx, logo.ID)
	})
}

// release removes the image binding the file and begins the file's delete,
// the delete protocol's first transaction, and returns the file deleting
// for the caller to purge after its commit.
func (s *store) release(ctx context.Context, tx *sqlate.Tx, fileID string) (blobfs.File, error) {
	if err := s.detach(ctx, tx, fileID); err != nil {
		return blobfs.File{}, err
	}
	return s.storage.FS.Files.Delete(ctx, tx, fileID)
}
