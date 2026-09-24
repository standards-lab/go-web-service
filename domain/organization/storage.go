package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

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
// replaces. The write's steps each run on their own session: the pending
// row and its inactive image commit together before any byte is stored, the
// put runs outside any transaction, and the completion on the pool. A put
// or a completion that fails leaves the pending row and its image,
// blobfs's pending state, for a sweep to remove.
func (s *store) putLogo(ctx context.Context, organizationID string, u web.Upload, ext string) (Identity, error) {
	fs, objects := s.storage.FS, s.storage.Objects
	// On the pool, Ensure finds the directory a concurrent first upload
	// created between its lookup and its insert.
	dir, _, err := fs.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory)
	if err != nil {
		return Identity{}, err
	}
	id := blobfs.NewID()
	file, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		// A nonexistent organization is the missing row, before the
		// image's foreign key would refuse it as a conflict.
		if _, err := s.view.One(ctx, tx, "id", organizationID); err != nil {
			return blobfs.File{}, err
		}
		file, err := fs.Files.Create(ctx, tx, objects, dir.ID, id+ext, u.ContentType, bfdata.WithID(id))
		if err != nil {
			return blobfs.File{}, err
		}
		return file, s.attach(ctx, tx, organizationID, file.ID)
	})
	if err != nil {
		return Identity{}, err
	}
	obj, err := objects.Put(ctx, file.Key, u.Body, u.ContentType, u.Size)
	if err != nil {
		return Identity{}, err
	}
	if file, err = fs.Files.Complete(ctx, s.db, file.ID, file.Version, obj); err != nil {
		return Identity{}, err
	}
	previous, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (string, error) {
		if err := fs.Files.Hold(ctx, tx, file.ID); err != nil {
			return "", err
		}
		current, err := s.activeLogo(ctx, tx, organizationID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		return current.ID, s.activate(ctx, tx, organizationID, file.ID)
	})
	if err != nil {
		// The activation failed, as a concurrent replacement that activated
		// its logo first does with a unique violation; the new file is
		// retired so it does not linger.
		return Identity{}, errors.Join(err, s.retire(ctx, func(*sqlate.Tx) (string, error) { return file.ID, nil }))
	}
	if previous != "" {
		// The new logo is active already, so a failed retire is the
		// request's error though the replacement stands; the replaced
		// file's steps converge when run again.
		if err := s.retire(ctx, func(*sqlate.Tx) (string, error) { return previous, nil }); err != nil {
			return Identity{}, fmt.Errorf("retire the replaced logo %s: %w", previous, err)
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
	if file.Status != blobfs.StatusAvailable || file.Size == nil || file.ETag == nil {
		return Logo{}, fmt.Errorf("logo of %s: file %s is %s: %w", organizationID, file.ID, file.Status, blobfs.ErrNotFound)
	}
	return Logo{
		Object: web.Object{ContentType: file.ContentType, Size: *file.Size, ETag: *file.ETag, ModifiedAt: file.UpdatedAt},
		Open:   func() (io.ReadCloser, error) { return s.storage.Objects.Open(ctx, file.Key) },
	}, nil
}

// deleteLogo retires the organization's active logo; none is sql.ErrNoRows.
func (s *store) deleteLogo(ctx context.Context, organizationID string) error {
	return s.retire(ctx, func(tx *sqlate.Tx) (string, error) {
		logo, err := s.activeLogo(ctx, tx, organizationID)
		return logo.ID, err
	})
}

// retire runs the delete protocol over the file pick names in its
// transaction: the file's image removed and blobfs's delete begun in that
// transaction, then the object deleted, then the row purged on the pool.
// Every step converges on a retry.
func (s *store) retire(ctx context.Context, pick func(*sqlate.Tx) (string, error)) error {
	fs := s.storage.FS
	file, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		id, err := pick(tx)
		if err != nil {
			return blobfs.File{}, err
		}
		if err := s.detach(ctx, tx, id); err != nil {
			return blobfs.File{}, err
		}
		return fs.Files.Delete(ctx, tx, id)
	})
	if err != nil {
		return err
	}
	if err := s.storage.Objects.Delete(ctx, file.Key); err != nil {
		return err
	}
	return fs.Files.Purge(ctx, s.db, file.ID)
}
