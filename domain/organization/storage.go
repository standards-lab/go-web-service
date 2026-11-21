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
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/data"
)

const imagesDirectory = "organization-images"

func (s *store) putLogo(ctx context.Context, organizationID string, u web.Upload, ext string) (LogoIdentity, error) {
	st := s.storage
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory, bfdata.CreateOptions{})
	if err != nil {
		return LogoIdentity{}, err
	}
	id := blobfs.NewID()
	file, err := st.FS.WriteFile(ctx, bfdata.WriteFileRequest{
		DB: s.db.DB, Objects: st.Objects, Body: u.Body, Size: u.Size,
		Begin: func(tx *sqlate.Tx) (blobfs.File, error) {
			if err := s.exists(ctx, tx, organizationID); err != nil {
				return blobfs.File{}, err
			}
			spec := bfdata.FileSpec{DirectoryID: dir.ID, Name: id + ext, ContentType: u.ContentType}
			return st.FS.Files.Create(ctx, tx, st.Objects, spec, bfdata.CreateOptions{ID: id})
		},
	})
	if err != nil {
		return LogoIdentity{}, err
	}
	cleanup := context.WithoutCancel(ctx)
	replaced, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (blobfs.File, error) {
		if err := st.FS.Files.Hold(ctx, tx, file.ID, bfdata.VersionOptions{}); err != nil {
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
		if err := s.attach(ctx, tx, organizationID, file.ID); err != nil {
			return blobfs.File{}, err
		}
		return current, raiseUploaded(q, organizationID, file)
	}), nil)
	if err != nil {
		return LogoIdentity{}, abandoned(err, st.FS.RemoveFileID(cleanup, s.db.DB, st.Objects, file.ID, bfdata.VersionOptions{}))
	}
	if replaced.ID != "" {
		s.purge(cleanup, organizationID, replaced)
	}
	return LogoIdentity{ID: file.ID}, nil
}

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

func (s *store) deleteLogo(ctx context.Context, organizationID string) error {
	deleting, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (blobfs.File, error) {
		logo, err := s.activeLogo(ctx, tx, organizationID)
		if err != nil {
			return blobfs.File{}, err
		}
		deleting, err := s.release(ctx, tx, logo.ID)
		if err != nil {
			return blobfs.File{}, err
		}
		LogoDeleted.Raise(q, deleting.ID, LogoDeletedData{FileID: deleting.ID, OrganizationID: organizationID, Version: deleting.Version})
		return deleting, nil
	}), nil)
	if err != nil {
		return err
	}
	s.purge(context.WithoutCancel(ctx), organizationID, deleting)
	return nil
}

func (s *store) purge(ctx context.Context, organizationID string, file blobfs.File) {
	if err := s.storage.FS.PurgeFile(ctx, s.db.DB, s.storage.Objects, file); err != nil {
		s.logger.WarnContext(ctx, "logo purge failed; the sweep's stale reclaim finishes it",
			"organization", organizationID, "file", file.ID, "error", err)
	}
}

func (s *store) release(ctx context.Context, tx *sqlate.Tx, fileID string) (blobfs.File, error) {
	if err := s.detach(ctx, tx, fileID); err != nil {
		return blobfs.File{}, err
	}
	return s.storage.FS.Files.Delete(ctx, tx, fileID, bfdata.VersionOptions{})
}

type logoSeed struct{ store *store }

var _ data.FileSeed = logoSeed{}

// Key names the logos in a state file and in the seed's counts.
func (logoSeed) Key() string { return "logos" }

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
	dir, _, err := st.FS.Directories.Ensure(ctx, s.db, blobfs.RootID, imagesDirectory, bfdata.CreateOptions{})
	if err != nil {
		return false, err
	}
	file, stored, err := st.FS.EnsureFile(ctx, bfdata.EnsureFileRequest{
		DB: s.db.DB, Objects: st.Objects, ID: l.ID,
		Body: bytes.NewReader(body), Size: int64(len(body)),
		Begin: func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
			spec := bfdata.FileSpec{DirectoryID: dir.ID, Name: l.ID + ext, ContentType: contentType}
			return st.FS.Files.Ensure(ctx, tx, st.Objects, spec, bfdata.CreateOptions{ID: l.ID})
		},
	})
	switch {
	case errors.Is(err, blobfs.ErrDeleting), errors.Is(err, blobfs.ErrIDTaken), errors.Is(err, blobfs.ErrNameTaken):
		return false, nil
	case err != nil:
		return false, err
	}
	var attached bool
	active, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (string, error) {
		if err := st.FS.Files.Hold(ctx, tx, file.ID, bfdata.VersionOptions{}); err != nil {
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
		if err := s.attach(ctx, tx, org.ID, file.ID); err != nil {
			return "", err
		}
		return file.ID, raiseUploaded(q, org.ID, file)
	}), nil)
	var ce *sqlate.ConstraintError
	switch {
	case err == nil && active == file.ID:
		return attached, nil
	case errors.Is(err, blobfs.ErrDeleting):
		return false, nil
	case errors.As(err, &ce) && ce.Class == sqlate.ErrUniqueViolation:
		switch ce.Constraint {
		case constraintImageFile:
			return false, nil
		case constraintImageActive:
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
	return false, abandoned(err, st.FS.RemoveFileID(ctx, s.db.DB, st.Objects, file.ID, bfdata.VersionOptions{Version: file.Version}))
}

func abandoned(err, removeErr error) error {
	if errors.Is(removeErr, blobfs.ErrNotFound) {
		return err
	}
	return errors.Join(err, removeErr)
}

const (
	constraintImageFile   = "uq_organization_image_file"
	constraintImageActive = "ux_organization_image_active"
)
