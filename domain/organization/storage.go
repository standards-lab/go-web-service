package organization

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/data"
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
// The completed file is then activated in one transaction, which holds the
// file, so no delete begins under the reference; removes the replaced
// logo's image and begins its delete; and inserts the file's image as the
// organization's active one. The replaced file is then purged, its object
// and its row. A concurrent replacement that activated first fails the
// insert with a unique violation; the new file is retired so it does not
// linger.
func (s *store) putLogo(ctx context.Context, organizationID string, u web.Upload, ext string) (LogoIdentity, error) {
	st := s.storage
	// On the pool, Ensure finds the directory a concurrent first upload
	// created between its lookup and its insert.
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory)
	if err != nil {
		return LogoIdentity{}, err
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
		return LogoIdentity{}, err
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
		return LogoIdentity{}, errors.Join(err, st.Retire(ctx, s.db.DB, func(*sqlate.Tx) (string, error) { return file.ID, nil }))
	}
	if replaced.ID != "" {
		// The new logo is active and the replaced file unreferenced and
		// deleting, so a failed purge is the request's error though the
		// replacement stands; the stale reclaim finishes the file if no
		// retry does.
		if err := st.Purge(ctx, s.db.DB, replaced); err != nil {
			return LogoIdentity{}, fmt.Errorf("retire the replaced logo %s: %w", replaced.ID, err)
		}
	}
	return LogoIdentity{ID: file.ID}, nil
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

// logoSeed is the layer's seed contribution, a file seed, to the data
// package's named states: the logos a state carries under "logos". The
// seeder runs it after the seed's transaction commits, when the
// organizations already stand, since a logo's write puts its object
// outside any transaction.
type logoSeed struct{ store *store }

var _ data.FileSeed = logoSeed{}

// Key names the logos in a state file and in the seed's counts.
func (logoSeed) Key() string { return "logos" }

// Verify prepares the layer's statements and blobfs's, which the logo's
// write and activation run.
func (s logoSeed) Verify(ctx context.Context) error {
	return errors.Join(s.store.Verify(ctx), s.store.storage.FS.Verify(ctx, s.store.db))
}

// Write seeds each logo in file order and returns how many it activated.
func (s logoSeed) Write(ctx context.Context, raw json.RawMessage, fixtures fs.FS) (int, error) {
	rows, err := data.SeedRows[logoSeedRow](raw)
	if err != nil {
		return 0, fmt.Errorf("seed logos: %w", err)
	}
	seeded := 0
	for _, l := range rows {
		ok, err := s.store.seedLogo(ctx, l, fixtures)
		if err != nil {
			return seeded, fmt.Errorf("seed logo of %s: %w", l.Organization, err)
		}
		if ok {
			seeded++
		}
	}
	return seeded, nil
}

// seedLogo makes the fixture the organization's active logo unless it has
// one, and reports whether it did. The fixture passes the upload's rules,
// the size bound and the allowlist by its sniffed type, before any I/O.
// An organization with an active logo, the seed's own from an earlier run
// or one a client uploaded, is left alone. Otherwise the file is written
// under the row's id by the write protocol's retry-safe form, which finds
// the file an interrupted run completed or resumes one it left pending,
// and is activated as the organization's logo only if none became active
// meanwhile.
//
// What the seed does not own it leaves as it stands: a file found under
// the id that another organization's image binds, one whose organization
// a client renamed or moved so the state's path now names a new row; a
// row that holds the id under another name; a file whose delete is under
// way, which the sweep finishes and the next seed writes again; and a
// file a concurrent seed activated first. Only a file this run stored and
// could not activate is retired, as a lost replacement is, and at the
// version it completed at, so no delete begins on a file an image may
// reference.
func (s *store) seedLogo(ctx context.Context, l logoSeedRow, fixtures fs.FS) (bool, error) {
	body, err := fs.ReadFile(fixtures, l.Fixture)
	if err != nil {
		return false, err
	}
	if len(body) > maxLogoBody {
		return false, fmt.Errorf("fixture %s is %d bytes, over the logo's %d", l.Fixture, len(body), maxLogoBody)
	}
	contentType := http.DetectContentType(body)
	ext, err := logoExtension(contentType)
	if err != nil {
		return false, fmt.Errorf("fixture %s: %w", l.Fixture, err)
	}
	org, err := s.view.One(ctx, s.db, "path", l.Organization)
	if err != nil {
		return false, fmt.Errorf("organization: %w", err)
	}
	switch _, err := s.activeLogo(ctx, s.db, org.ID); {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	st := s.storage
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory)
	if err != nil {
		return false, err
	}
	file, stored, err := st.Ensure(ctx, s.db.DB, bytes.NewReader(body), int64(len(body)), func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
		return st.FS.Files.Ensure(ctx, tx, st.Objects, dir.ID, l.ID+ext, contentType, bfdata.WithID(l.ID))
	})
	switch {
	case errors.Is(err, blobfs.ErrDeleting), errors.Is(err, blobfs.ErrIDTaken):
		return false, nil
	case err != nil:
		return false, err
	}
	// active is the organization's active logo once the activation
	// commits, the seeded file when this run attached it.
	var attached bool
	active, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (string, error) {
		if err := st.FS.Files.Hold(ctx, tx, file.ID); err != nil {
			return "", err
		}
		current, err := s.activeLogo(ctx, tx, org.ID)
		switch {
		case err == nil:
			return current.ID, nil
		case !errors.Is(err, sql.ErrNoRows):
			return "", err
		}
		attached = true
		return file.ID, s.attach(ctx, tx, org.ID, file.ID)
	})
	var ce *sqlate.ConstraintError
	switch {
	case err == nil && active == file.ID:
		return attached, nil
	case errors.Is(err, blobfs.ErrDeleting):
		return false, nil
	case errors.Is(err, sqlate.ErrUniqueViolation):
		// A concurrent seed activated the same file first, or another
		// organization's image binds the file found under the id.
		if current, rerr := s.activeLogo(ctx, s.db, org.ID); rerr == nil && current.ID == file.ID {
			return false, nil
		}
		if errors.As(err, &ce) && ce.Constraint == constraintImageFile {
			return false, nil
		}
	}
	if !stored {
		return false, err
	}
	// The activation rolled back, or another logo became active since the
	// check and is left alone: no image references the file this run
	// stored, so it is retired, at the version it completed at.
	return false, errors.Join(err, st.Retire(ctx, s.db.DB, func(*sqlate.Tx) (string, error) { return file.ID, nil }, bfdata.AtVersion(file.Version)))
}

// constraintImageFile is the unique constraint that admits one image per
// file, which a seeded file another organization's image binds violates.
const constraintImageFile = "uq_organization_image_file"
