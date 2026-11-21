package document

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/data"
)

func listing[T any](ctx context.Context, sess sqlate.Session, l bfdata.Listing[T], id string, q web.Query) ([]T, web.Paging, error) {
	c, err := data.ReadListing(ctx, sess, l, id, q)
	if errors.Is(err, blobfs.ErrDeleting) {
		return nil, web.Paging{}, fmt.Errorf("directory %s is being deleted: %w", id, blobfs.ErrNotFound)
	}
	return c.Items, data.Paging(c), err
}

func (s *store) scope(ctx context.Context, sess sqlate.Session, organizationID, id string) (root, dir string, err error) {
	if root, err = s.documentRoot(ctx, sess, organizationID); err != nil {
		return "", "", fmt.Errorf("document root of organization %s: %w", organizationID, err)
	}
	dir, err = s.within(ctx, sess, root, id)
	return root, dir, err
}

func (s *store) within(ctx context.Context, sess sqlate.Session, root, id string) (string, error) {
	if id == RootAlias || id == root {
		return root, nil
	}
	in, err := s.storage.FS.Directories.IsWithin(ctx, sess, id, root)
	if err != nil {
		return "", err
	}
	if !in {
		return "", fmt.Errorf("directory %s: %w", id, blobfs.ErrNotFound)
	}
	return id, nil
}

func (s *store) fileScope(ctx context.Context, sess sqlate.Session, organizationID, id string) (string, blobfs.File, error) {
	root, err := s.documentRoot(ctx, sess, organizationID)
	if err != nil {
		return "", blobfs.File{}, fmt.Errorf("document root of organization %s: %w", organizationID, err)
	}
	file, err := s.storage.FS.Files.Find(ctx, sess, id)
	if err != nil {
		return "", blobfs.File{}, err
	}
	if _, err := s.within(ctx, sess, root, file.DirectoryID); err != nil {
		return "", blobfs.File{}, fmt.Errorf("file %s: %w", id, blobfs.ErrNotFound)
	}
	return root, file, nil
}

func (s *store) writable(ctx context.Context, organizationID, id string) (string, error) {
	if id != RootAlias {
		return id, nil
	}
	return s.ensureRoot(ctx, organizationID, bfdata.CreateOptions{})
}

func (s *store) ensureRoot(ctx context.Context, organizationID string, opts bfdata.CreateOptions) (string, error) {
	root, err := s.documentRoot(ctx, s.db, organizationID)
	if !errors.Is(err, sql.ErrNoRows) {
		return root, err
	}
	root, err = s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (string, error) {
		if err := s.organizationExists(ctx, tx, organizationID); err != nil {
			return "", fmt.Errorf("organization %s: %w", organizationID, err)
		}
		dir, created, err := s.storage.FS.Directories.Ensure(ctx, tx, blobfs.RootID, organizationID, opts)
		if err != nil {
			return "", err
		}
		if err := s.bind(ctx, tx, organizationID, dir.ID); err != nil {
			return "", err
		}
		if created {
			raiseDirectory(q, DirectoryCreated, organizationID, dir.ID, dir)
		}
		return dir.ID, nil
	}), nil)
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, sqlate.ErrUniqueViolation) {
		return s.documentRoot(ctx, s.db, organizationID)
	}
	return root, err
}

func (s *store) createDirectory(ctx context.Context, organizationID string, c CreateDirectory) (Identity, error) {
	parent, err := s.writable(ctx, organizationID, c.ParentID)
	if err != nil {
		return Identity{}, err
	}
	dir, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (blobfs.Directory, error) {
		root, parent, err := s.scope(ctx, tx, organizationID, parent)
		if err != nil {
			return blobfs.Directory{}, err
		}
		dir, err := s.storage.FS.Directories.Create(ctx, tx, parent, c.Name, bfdata.CreateOptions{})
		if err != nil {
			return blobfs.Directory{}, err
		}
		raiseDirectory(q, DirectoryCreated, organizationID, root, dir)
		return dir, nil
	}), nil)
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

func (s *store) directory(ctx context.Context, organizationID, id string) (Directory, error) {
	fs := s.storage.FS
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (Directory, error) {
		root, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return Directory{}, err
		}
		d, err := fs.Directories.Find(ctx, tx, id)
		if err != nil {
			return Directory{}, err
		}
		path, err := fs.Directories.Path(ctx, tx, id)
		if err != nil {
			return Directory{}, err
		}
		out, err := directoryOf(d, root)
		if err != nil {
			return Directory{}, err
		}
		_, below, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		out.Path = "/" + below
		return out, nil
	}, nil)
}

func (s *store) listDirectories(ctx context.Context, organizationID, id string, q web.Query) ([]Directory, web.Paging, error) {
	root, dir, err := s.listScope(ctx, organizationID, id)
	if err != nil {
		return nil, web.Paging{}, err
	}
	items, paging, err := listing(ctx, s.db, s.storage.FS.Directories, dir, q)
	if err != nil {
		return nil, web.Paging{}, err
	}
	out, err := present(items, func(d blobfs.Directory) (Directory, error) { return directoryOf(d, root) })
	return out, paging, err
}

func (s *store) listFiles(ctx context.Context, organizationID, id string, q web.Query) ([]File, web.Paging, error) {
	_, dir, err := s.listScope(ctx, organizationID, id)
	if err != nil {
		return nil, web.Paging{}, err
	}
	items, paging, err := listing(ctx, s.db, s.storage.FS.Files, dir, q)
	if err != nil {
		return nil, web.Paging{}, err
	}
	out, err := present(items, fileOf)
	return out, paging, err
}

func (s *store) listScope(ctx context.Context, organizationID, id string) (root, dir string, err error) {
	root, dir, err = s.scope(ctx, s.db, organizationID, id)
	if id != RootAlias || !errors.Is(err, sql.ErrNoRows) {
		return root, dir, err
	}
	if err := s.organizationExists(ctx, s.db, organizationID); err != nil {
		return "", "", fmt.Errorf("organization %s: %w", organizationID, err)
	}
	none := blobfs.NewID()
	return none, none, nil
}

func (s *store) deleteDirectory(ctx context.Context, organizationID, id string, version int64) error {
	_, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (struct{}, error) {
		_, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return struct{}{}, err
		}
		if err := s.storage.FS.Directories.Delete(ctx, tx, id, bfdata.VersionOptions{Version: version}); err != nil {
			return struct{}{}, err
		}
		DirectoryDeleted.Raise(q, id, DirectoryDeletedData{DirectoryID: id, OrganizationID: organizationID, Version: version})
		return struct{}{}, nil
	}), nil)
	return err
}

func (s *store) markBranch(ctx context.Context, organizationID, id string, version int64) (string, error) {
	return s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (string, error) {
		_, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return "", err
		}
		if err := s.storage.FS.Directories.LockTree(ctx, tx); err != nil {
			return "", err
		}
		anchor, err := s.storage.FS.Directories.Find(ctx, tx, id)
		if err != nil {
			return "", err
		}
		marked, err := s.storage.FS.Directories.MarkDeleting(ctx, tx, id, bfdata.VersionOptions{Version: version})
		if err != nil || anchor.Status != blobfs.DirectoryStatusActive || marked.Directories == 0 {
			return id, err
		}
		return id, s.raiseMarked(ctx, tx, q, organizationID, id)
	}), nil)
}

func (s *store) moveDirectory(ctx context.Context, organizationID, id string, version int64, m MoveDirectory) (Identity, error) {
	dir, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (blobfs.Directory, error) {
		root, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return blobfs.Directory{}, err
		}
		if id == root {
			return blobfs.Directory{}, errRootMove
		}
		parent, err := s.within(ctx, tx, root, m.ParentID)
		if err != nil {
			return blobfs.Directory{}, err
		}
		dir, err := s.storage.FS.Directories.Move(ctx, tx, id,
			bfdata.DirectoryMove{ParentID: parent, Name: m.Name, Version: version})
		if err != nil {
			return blobfs.Directory{}, err
		}
		raiseDirectory(q, DirectoryMoved, organizationID, root, dir)
		return dir, nil
	}), nil)
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

func (s *store) uploadFile(ctx context.Context, organizationID, directoryID, name string, u web.Upload) (Identity, error) {
	st := s.storage
	directoryID, err := s.writable(ctx, organizationID, directoryID)
	if err != nil {
		return Identity{}, err
	}
	file, err := st.FS.WriteFile(ctx, bfdata.WriteFileRequest{
		DB: s.db.DB, Objects: st.Objects, Body: u.Body, Size: u.Size,
		Begin: func(tx *sqlate.Tx) (blobfs.File, error) {
			_, dir, err := s.scope(ctx, tx, organizationID, directoryID)
			if err != nil {
				return blobfs.File{}, err
			}
			spec := bfdata.FileSpec{DirectoryID: dir, Name: name, ContentType: u.ContentType}
			return st.FS.Files.Create(ctx, tx, st.Objects, spec, bfdata.CreateOptions{})
		},
		OnComplete: func(ctx context.Context, tx *sqlate.Tx, f blobfs.File) error {
			return s.emitUploaded(ctx, tx, organizationID, f)
		},
	})
	return Identity{ID: file.ID, Version: file.Version}, err
}

func (s *store) file(ctx context.Context, organizationID, id string) (File, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	if err != nil {
		return File{}, err
	}
	return fileOf(file)
}

func (s *store) content(ctx context.Context, organizationID, id string) (Content, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	if err != nil {
		return Content{}, err
	}
	dl, err := s.storage.Serve(ctx, file)
	if err != nil {
		return Content{}, fmt.Errorf("content: %w", err)
	}
	return Content{Name: file.Name, Object: dl.Object, Open: dl.Open}, nil
}

func (s *store) deleteFile(ctx context.Context, organizationID, id string, version int64) error {
	st := s.storage
	return st.FS.RemoveFile(ctx, s.db.DB, st.Objects, func(tx *sqlate.Tx) (string, error) {
		_, file, err := s.fileScope(ctx, tx, organizationID, id)
		return file.ID, err
	}, bfdata.RemoveOptions{Version: version, OnDelete: func(ctx context.Context, tx *sqlate.Tx, f blobfs.File) error {
		return s.emitDeleted(ctx, tx, organizationID, f)
	}})
}

func (s *store) moveFile(ctx context.Context, organizationID, id string, version int64, m MoveFile) (Identity, error) {
	file, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (blobfs.File, error) {
		root, _, err := s.fileScope(ctx, tx, organizationID, id)
		if err != nil {
			return blobfs.File{}, err
		}
		dir, err := s.within(ctx, tx, root, m.DirectoryID)
		if err != nil {
			return blobfs.File{}, err
		}
		file, err := s.storage.FS.Files.Move(ctx, tx, id,
			bfdata.FileMove{DirectoryID: dir, Name: m.Name, Version: version})
		if err != nil {
			return blobfs.File{}, err
		}
		FileMoved.Raise(q, file.ID, fileData(organizationID, file))
		return file, nil
	}), nil)
	return Identity{ID: file.ID, Version: file.Version}, err
}

type seed struct{ store *store }

var _ data.FileSeed = seed{}

// Key names the hierarchies in a state file and in the seed's counts.
func (seed) Key() string { return "documents" }

// Write seeds each hierarchy in file order and returns how many entries,
// directories and files, it created. The files' bytes are inline, so the
// fixtures go unread.
func (s seed) Write(ctx context.Context, raw json.RawMessage, _ fs.FS) (int, error) {
	trees, err := data.SeedRows[seedTree](raw)
	if err != nil {
		return 0, fmt.Errorf("seed documents: %w", err)
	}
	created := 0
	for _, t := range trees {
		n, err := s.store.seedTree(ctx, t)
		created += n
		if err != nil {
			return created, fmt.Errorf("seed documents of %s: %w", t.Organization, err)
		}
	}
	return created, nil
}

func (s *store) seedTree(ctx context.Context, t seedTree) (int, error) {
	org, err := s.seedOrganization(ctx, s.db, t.Organization)
	if err != nil {
		return 0, err
	}
	root, err := s.ensureRoot(ctx, org, bfdata.CreateOptions{ID: t.Root})
	if err != nil {
		return 0, err
	}
	return s.seedEntries(ctx, org, root, root, t.Entries)
}

func (s *store) seedEntries(ctx context.Context, organizationID, root, parent string, entries []seedEntry) (int, error) {
	created := 0
	for _, e := range entries {
		n, err := s.seedEntry(ctx, organizationID, root, parent, e)
		created += n
		if err != nil {
			return created, fmt.Errorf("%s: %w", e.Name, err)
		}
	}
	return created, nil
}

func (s *store) seedEntry(ctx context.Context, organizationID, root, parent string, e seedEntry) (int, error) {
	st := s.storage
	isDir, err := e.directory()
	if err != nil {
		return 0, err
	}
	if isDir {
		dir, created, err := s.seedDirectory(ctx, organizationID, root, parent, e)
		if errors.Is(err, blobfs.ErrIDTaken) || errors.Is(err, blobfs.ErrDeleting) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		n, err := s.seedEntries(ctx, organizationID, root, dir.ID, e.Entries)
		if created {
			n++
		}
		return n, err
	}
	_, stored, err := st.FS.EnsureFile(ctx, bfdata.EnsureFileRequest{
		DB: s.db.DB, Objects: st.Objects, ID: e.ID,
		Body: strings.NewReader(e.Content), Size: int64(len(e.Content)),
		Begin: func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
			spec := bfdata.FileSpec{DirectoryID: parent, Name: e.Name, ContentType: e.ContentType}
			return st.FS.Files.Ensure(ctx, tx, st.Objects, spec, bfdata.CreateOptions{ID: e.ID})
		},
		OnComplete: func(ctx context.Context, tx *sqlate.Tx, f blobfs.File) error {
			return s.emitUploaded(ctx, tx, organizationID, f)
		},
	})
	switch {
	case errors.Is(err, blobfs.ErrIDTaken), errors.Is(err, blobfs.ErrNameTaken), errors.Is(err, blobfs.ErrDeleting):
		return 0, nil
	case err != nil:
		return 0, err
	case stored:
		return 1, nil
	}
	return 0, nil
}

func (s *store) seedDirectory(ctx context.Context, organizationID, root, parent string, e seedEntry) (blobfs.Directory, bool, error) {
	type ensured struct {
		dir     blobfs.Directory
		created bool
	}
	ensure := s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (ensured, error) {
		dir, created, err := s.storage.FS.Directories.Ensure(ctx, tx, parent, e.Name, bfdata.CreateOptions{ID: e.ID})
		if err != nil {
			return ensured{}, err
		}
		if created {
			raiseDirectory(q, DirectoryCreated, organizationID, root, dir)
		}
		return ensured{dir, created}, nil
	})
	r, err := s.db.Transact(ctx, ensure, nil)
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, blobfs.ErrIDTaken) {
		r, err = s.db.Transact(ctx, ensure, nil)
	}
	return r.dir, r.created, err
}

func directoryOf(d blobfs.Directory, root string) (Directory, error) {
	status, err := directoryStatus(d.Status)
	if err != nil {
		return Directory{}, fmt.Errorf("directory %s: %w", d.ID, err)
	}
	out := Directory{ID: d.ID, ParentID: d.ParentID, Name: d.Name, Status: status, Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	if d.ID == root {
		out.ParentID, out.Name = nil, "/"
	}
	return out, nil
}

func fileOf(f blobfs.File) (File, error) {
	status, err := fileStatus(f.Status)
	if err != nil {
		return File{}, fmt.Errorf("file %s: %w", f.ID, err)
	}
	return File{
		ID: f.ID, DirectoryID: f.DirectoryID, Name: f.Name, Status: status, Size: f.Size,
		ContentType: f.ContentType, Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}, nil
}

func present[T, U any](items []T, of func(T) (U, error)) ([]U, error) {
	out := make([]U, len(items))
	for i, item := range items {
		var err error
		if out[i], err = of(item); err != nil {
			return nil, err
		}
	}
	return out, nil
}

var errUnknownStatus = errors.New("document: a blobfs status the layer does not name")

func directoryStatus(s blobfs.DirectoryStatus) (DirectoryStatus, error) {
	switch s {
	case blobfs.DirectoryStatusActive:
		return DirectoryActive, nil
	case blobfs.DirectoryStatusDeleting:
		return DirectoryDeleting, nil
	}
	return "", fmt.Errorf("directory status %q: %w", s, errUnknownStatus)
}

func fileStatus(s blobfs.Status) (FileStatus, error) {
	switch s {
	case blobfs.StatusPending:
		return FilePending, nil
	case blobfs.StatusAvailable:
		return FileAvailable, nil
	case blobfs.StatusDeleting:
		return FileDeleting, nil
	}
	return "", fmt.Errorf("file status %q: %w", s, errUnknownStatus)
}
