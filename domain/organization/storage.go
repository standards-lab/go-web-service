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
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

// imagesDirectory is the structural directory under blobfs's root that
// holds every organization's image files. Ownership is at the file grain,
// the organization_image row, so the directory binds no organization.
const imagesDirectory = "organization-images"

// putLogo stores the upload as the organization's logo, named for its new
// file id with ext, and makes it the active one. blobfs's two-phase write
// stores the file, its pending row created once the organization is read,
// so a missing organization is sql.ErrNoRows before any byte is stored; no
// image references the pending row, so a write that stops partway leaves
// only a row the stale reclaim removes.
//
// One transaction then holds the completed file, releases the replaced
// logo (its image removed, its file's delete begun), and binds the new file
// as the active image. A concurrent replacement that activated first fails
// the bind with a unique violation, and the new file is retired. Once the
// activation commits, the replaced file is purged; a purge that fails is
// logged and the request succeeds, since the replacement stands and the
// stale reclaim finishes the deleting row.
//
// The steps after the write run on a context the request's cancellation
// does not reach: an available file no image references is one the stale
// reclaim, which reaches only pending and deleting rows, never removes.
func (s *store) putLogo(ctx context.Context, organizationID string, u web.Upload, ext string) (LogoIdentity, error) {
	st := s.storage
	// On the pool, Ensure finds the directory a concurrent first upload
	// created between its lookup and its insert.
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory)
	if err != nil {
		return LogoIdentity{}, err
	}
	id := blobfs.NewID()
	file, err := st.FS.WriteFile(ctx, s.db.DB, st.Objects, u.Body, u.Size, func(tx *sqlate.Tx) (blobfs.File, error) {
		// A nonexistent organization is the missing row, before the
		// activation's foreign key would refuse it as a conflict.
		if err := s.exists(ctx, tx, organizationID); err != nil {
			return blobfs.File{}, err
		}
		return st.FS.Files.Create(ctx, tx, st.Objects, dir.ID, id+ext, u.ContentType, bfdata.WithID(id))
	})
	if err != nil {
		return LogoIdentity{}, err
	}
	cleanup := context.WithoutCancel(ctx)
	replaced, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		// A hold refused as deleting is blobfs's blobfs.DeletingError,
		// which names the file's own delete apart from its directory's.
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
		return LogoIdentity{}, abandoned(err, st.FS.RemoveFileID(cleanup, s.db.DB, st.Objects, file.ID))
	}
	if replaced.ID != "" {
		s.purge(cleanup, organizationID, replaced)
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
	dl, err := s.storage.Serve(ctx, file)
	if err != nil {
		return Logo{}, fmt.Errorf("logo of %s: %w", organizationID, err)
	}
	return Logo(dl), nil
}

// deleteLogo retires the organization's active logo by blobfs's two-phase
// delete: one transaction removes its image and begins its file's delete,
// and the file is purged once that commits; none is sql.ErrNoRows. A purge
// that fails is logged and the delete succeeds, as putLogo's does.
func (s *store) deleteLogo(ctx context.Context, organizationID string) error {
	deleting, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		logo, err := s.activeLogo(ctx, tx, organizationID)
		if err != nil {
			return blobfs.File{}, err
		}
		return s.release(ctx, tx, logo.ID)
	})
	if err != nil {
		return err
	}
	s.purge(context.WithoutCancel(ctx), organizationID, deleting)
	return nil
}

// purge removes a released logo's object and row, the delete's second
// phase, after the change that released it committed. A failure is logged,
// not returned: the change stands, and the stale reclaim finishes the
// deleting row. The record is logged under ctx, which keeps the request's
// values past its cancellation, so it carries the request's trace, whose
// id is the request's id.
func (s *store) purge(ctx context.Context, organizationID string, file blobfs.File) {
	if err := s.storage.FS.PurgeFile(ctx, s.db.DB, s.storage.Objects, file); err != nil {
		s.logger.WarnContext(ctx, "logo purge failed; the sweep's stale reclaim finishes it",
			"organization", organizationID, "file", file.ID, "error", err)
	}
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

// Verifiers are the layer's store and blobfs's, which the logo's write and
// activation run.
func (s logoSeed) Verifiers() []query.Verifier {
	return []query.Verifier{s.store, s.store.storage.FS}
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
// one, and reports whether it did. The fixture passes the upload's rules
// before any I/O. The file is written under the row's id by blobfs's
// Store.EnsureFile, which finds or resumes an interrupted run's file, and
// is activated only if no logo became active meanwhile.
//
// What the seed does not own it leaves as it stands, with no error: a file
// under the id that another organization's image binds, a row holding the
// id or the name otherwise, a file whose delete is under way, and a logo
// that became active first, a concurrent seed's or a client's. Only a file
// this run stored and could not activate is retired, at the version it
// completed at, so no delete begins on a file an image may reference.
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
	file, stored, err := st.FS.EnsureFile(ctx, s.db.DB, st.Objects, l.ID, bytes.NewReader(body), int64(len(body)), func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
		return st.FS.Files.Ensure(ctx, tx, st.Objects, dir.ID, l.ID+ext, contentType, bfdata.WithID(l.ID))
	})
	switch {
	case errors.Is(err, blobfs.ErrDeleting), errors.Is(err, blobfs.ErrIDTaken), errors.Is(err, blobfs.ErrNameTaken):
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
	case errors.As(err, &ce) && ce.Class == sqlate.ErrUniqueViolation:
		switch ce.Constraint {
		case constraintImageFile:
			// Another organization's image binds the file found under
			// the id.
			return false, nil
		case constraintImageActive:
			// Another logo won the activation since the check. A
			// concurrent seed that activated this same file is done;
			// any other logo is left alone, as the check's own finding
			// is, and the file this run stored is retired below. A
			// re-read that fails finds neither, and the file may be the
			// active one, so nothing is retired and the failure is the
			// seed's.
			current, rerr := s.activeLogo(ctx, s.db, org.ID)
			if rerr != nil {
				return false, fmt.Errorf("the active logo, after losing the activation: %w", rerr)
			}
			if current.ID == file.ID {
				return false, nil
			}
			err = nil
		}
	}
	if !stored {
		return false, err
	}
	// The activation rolled back, or another logo became active and is
	// left alone: no image references the file this run stored, so it is
	// retired, at the version it completed at.
	return false, abandoned(err, st.FS.RemoveFileID(ctx, s.db.DB, st.Objects, file.ID, bfdata.AtVersion(file.Version)))
}

// abandoned is the error of a write refused with err whose stored file was
// then removed, with removeErr the removal's own. A file the removal no
// longer finds is gone already, the stale reclaim having finished it, so
// the refusal alone answers: joined, the removal's blobfs.ErrNotFound would
// turn the refusal's 409 into a 404.
func abandoned(err, removeErr error) error {
	if errors.Is(removeErr, blobfs.ErrNotFound) {
		return err
	}
	return errors.Join(err, removeErr)
}

// The unique constraints of organization_image the logo seed tells apart:
// one image per file, which a seeded file another organization's image
// binds violates, and one active image per organization, which a logo that
// won the activation first violates.
const (
	constraintImageFile   = "uq_organization_image_file"
	constraintImageActive = "ux_organization_image_active"
)
