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
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/data"
)

// listing reads one page of blobfs's listing l, Directories' or Files',
// of the directory with id, as the request addressed it, through the data
// package's lowering. Deleting rows are hidden, and the listing of a
// directory in a branch being deleted is not found: blobfs refuses it as
// blobfs.ErrDeleting, which the layer reports as the missing directory
// rather than a conflict, since a mark is never undone, so nothing a
// client sends makes the directory listable again.
func listing[T any](ctx context.Context, sess sqlate.Session, l data.Listing[T], id string, q web.Query) ([]T, web.Paging, error) {
	c, err := data.ReadListing(ctx, sess, l, id, q)
	if errors.Is(err, blobfs.ErrDeleting) {
		return nil, web.Paging{}, fmt.Errorf("directory %s is being deleted: %w", id, blobfs.ErrNotFound)
	}
	return c.Items, data.Paging(c), err
}

// scope resolves the directory id names, the root alias or a directory id,
// within the organization's document root, and returns the root's id and
// the directory's. The owner row is read first, so an organization without
// a root is the missing row and an id outside it is not found, the answer
// an absent id gets: the supplied id is an input to check, never a fact to
// trust.
func (s *store) scope(ctx context.Context, sess sqlate.Session, organizationID, id string) (root, dir string, err error) {
	if root, err = s.documentRoot(ctx, sess, organizationID); err != nil {
		return "", "", fmt.Errorf("document root of organization %s: %w", organizationID, err)
	}
	dir, err = s.within(ctx, sess, root, id)
	return root, dir, err
}

// within resolves the directory id names under the root with id root: the
// alias is the root, and any other id must lie within it.
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

// fileScope reads the file with id when its directory lies within the
// organization's document root, and returns the root's id with it; the
// owner row is read first, as scope reads it.
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

// writable resolves the directory a write names, ensuring the document root
// first when the write names it by its alias.
func (s *store) writable(ctx context.Context, organizationID, id string) (string, error) {
	if id != RootAlias {
		return id, nil
	}
	return s.ensureRoot(ctx, organizationID)
}

// ensureRoot returns the organization's document root, creating it on the
// organization's first write: the top-level directory named with the
// organization's id and its owner row, in one transaction after the
// organization is read, so a nonexistent organization is the missing row.
// A concurrent first write that bound the root first fails this one's
// insert, and the root it bound is read on the pool. opts reach blobfs's
// insert, the seed's fixed id among them.
func (s *store) ensureRoot(ctx context.Context, organizationID string, opts ...bfdata.CreateOption) (string, error) {
	root, err := s.documentRoot(ctx, s.db, organizationID)
	if !errors.Is(err, sql.ErrNoRows) {
		return root, err
	}
	root, err = s.db.Transact(ctx, func(tx *sqlate.Tx) (string, error) {
		if err := s.organizationExists(ctx, tx, organizationID); err != nil {
			return "", fmt.Errorf("organization %s: %w", organizationID, err)
		}
		dir, _, err := s.storage.FS.Directories.Ensure(ctx, tx, blobfs.RootID, organizationID, opts...)
		if err != nil {
			return "", err
		}
		return dir.ID, s.bind(ctx, tx, organizationID, dir.ID)
	})
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, sqlate.ErrUniqueViolation) {
		return s.documentRoot(ctx, s.db, organizationID)
	}
	return root, err
}

// createDirectory creates a directory under the parent, within the root,
// ensuring the root when the parent is its alias.
func (s *store) createDirectory(ctx context.Context, organizationID string, c CreateDirectory) (Identity, error) {
	parent, err := s.writable(ctx, organizationID, c.ParentID)
	if err != nil {
		return Identity{}, err
	}
	dir, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.Directory, error) {
		_, parent, err := s.scope(ctx, tx, organizationID, parent)
		if err != nil {
			return blobfs.Directory{}, err
		}
		return s.storage.FS.Directories.Create(ctx, tx, parent, c.Name)
	})
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

// directory reads the directory with its path from the root: blobfs's path
// runs from its own root, and its first segment is the document root.
func (s *store) directory(ctx context.Context, organizationID, id string) (Directory, error) {
	fs := s.storage.FS
	root, id, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return Directory{}, err
	}
	d, err := fs.Directories.Find(ctx, s.db, id)
	if err != nil {
		return Directory{}, err
	}
	path, err := fs.Directories.Path(ctx, s.db, id)
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
}

// listDirectories reads one page of the directory's child directories. An
// organization without a root lists nothing under the alias.
func (s *store) listDirectories(ctx context.Context, organizationID, id string, q web.Query) ([]Directory, web.Paging, error) {
	root, dir, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return rootless[Directory](ctx, s, organizationID, id, err)
	}
	items, paging, err := listing(ctx, s.db, s.storage.FS.Directories, dir, q)
	if err != nil {
		return nil, web.Paging{}, err
	}
	out, err := present(items, func(d blobfs.Directory) (Directory, error) { return directoryOf(d, root) })
	return out, paging, err
}

// listFiles reads one page of the directory's files, pending and
// available; a file whose delete has begun is hidden, as every listing
// hides deleting rows.
func (s *store) listFiles(ctx context.Context, organizationID, id string, q web.Query) ([]File, web.Paging, error) {
	_, dir, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return rootless[File](ctx, s, organizationID, id, err)
	}
	items, paging, err := listing(ctx, s.db, s.storage.FS.Files, dir, q)
	if err != nil {
		return nil, web.Paging{}, err
	}
	out, err := present(items, fileOf)
	return out, paging, err
}

// rootless answers a listing's scope failure: an organization without a
// root yet has an empty hierarchy, so the alias lists as an empty, counted
// page once the organization is read, while a nonexistent organization is
// the missing row, as every other route answers it, and a specific id is
// not found. Any other failure is the request's.
func rootless[T any](ctx context.Context, s *store, organizationID, id string, err error) ([]T, web.Paging, error) {
	if id != RootAlias || !errors.Is(err, sql.ErrNoRows) {
		return nil, web.Paging{}, err
	}
	if err := s.organizationExists(ctx, s.db, organizationID); err != nil {
		return nil, web.Paging{}, fmt.Errorf("organization %s: %w", organizationID, err)
	}
	return nil, web.Paging{}, nil
}

// deleteDirectory removes the empty directory at version, its scope
// checked first. Removing the root removes its owner row with it, through
// the owner row's cascading foreign key, so the organization has no root
// until its next write ensures a new one.
func (s *store) deleteDirectory(ctx context.Context, organizationID, id string, version int64) error {
	_, id, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return err
	}
	return s.storage.FS.Directories.Delete(ctx, s.db, id, bfdata.AtVersion(version))
}

// markBranch begins the delete of the directory with everything beneath
// it: blobfs's mark, the directory at version, in one transaction with the
// scope check, and returns the directory's id. The sweep removes the
// branch after the commit. A directory deleting already is the mark's
// retry, which converges at any version.
func (s *store) markBranch(ctx context.Context, organizationID, id string, version int64) (string, error) {
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (string, error) {
		_, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return "", err
		}
		_, err = s.storage.FS.Directories.MarkDeleting(ctx, tx, id, bfdata.AtVersion(version))
		return id, err
	})
}

// moveDirectory moves the directory under a new parent within the same
// root, under blobfs's tree lock and cycle check and the version guard. The
// root itself stays where it is.
func (s *store) moveDirectory(ctx context.Context, organizationID, id string, version int64, m MoveDirectory) (Identity, error) {
	dir, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.Directory, error) {
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
		return s.storage.FS.Directories.Move(ctx, tx, id, parent, m.Name, version)
	})
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

// uploadFile stores the upload as a new file named name in the directory,
// ensuring the root when the directory is its alias, by the data package's
// write protocol: the scope check and the pending row commit together
// before any byte is stored, the put runs outside any transaction, and the
// completion runs on the pool. A put or a completion that fails retires the
// pending row, so its name is free for a retry; one whose retire fails too
// leaves the row for the sweep. A completion refused because the row's
// delete began or the sweep removed it, the mark of a branch that raced the
// write, deletes the object just put; the row is the sweep's. A completion
// refused because the stale reclaim began the row's own delete is the
// file deleting, told apart from the branch's mark as deletingFile tells
// it.
func (s *store) uploadFile(ctx context.Context, organizationID, directoryID, name string, u web.Upload) (Identity, error) {
	st := s.storage
	directoryID, err := s.writable(ctx, organizationID, directoryID)
	if err != nil {
		return Identity{}, err
	}
	var created string
	file, err := st.Write(ctx, s.db.DB, u.Body, u.Size, func(tx *sqlate.Tx) (blobfs.File, error) {
		_, dir, err := s.scope(ctx, tx, organizationID, directoryID)
		if err != nil {
			return blobfs.File{}, err
		}
		file, err := st.FS.Files.Create(ctx, tx, st.Objects, dir, name, u.ContentType)
		created = file.ID
		return file, err
	})
	if err != nil && created != "" {
		err = s.deletingFile(ctx, created, err)
	}
	return Identity{ID: file.ID, Version: file.Version}, err
}

// deletingFile tells a refusal blobfs reports as blobfs.ErrDeleting apart
// by reading the file with id: a file whose own delete began, in a
// directory that is not deleting, is data.ErrFileDeleting, while a file
// deleting because its branch is marked, or one not found deleting, keeps
// the refusal as the directory's. blobfs reports the file's own delete
// and a directory's as one error; once it types them apart, this read
// gives way to its own error. Any other error is returned as it is.
func (s *store) deletingFile(ctx context.Context, id string, err error) error {
	if !errors.Is(err, blobfs.ErrDeleting) {
		return err
	}
	fs := s.storage.FS
	file, ferr := fs.Files.Find(ctx, s.db, id)
	if ferr != nil || file.Status != blobfs.StatusDeleting {
		return err
	}
	if dir, derr := fs.Directories.Find(ctx, s.db, file.DirectoryID); derr != nil || dir.Status != blobfs.DirectoryStatusActive {
		return err
	}
	return fmt.Errorf("file %s: %w: %w", id, data.ErrFileDeleting, err)
}

// file reads the file's metadata.
func (s *store) file(ctx context.Context, organizationID, id string) (File, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	if err != nil {
		return File{}, err
	}
	return fileOf(file)
}

// content reads the file for its download. Only an available file is
// served; any other is not found, as an absent one is.
func (s *store) content(ctx context.Context, organizationID, id string) (Content, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	if err != nil {
		return Content{}, err
	}
	obj, open, err := s.storage.Serve(ctx, file)
	if err != nil {
		return Content{}, fmt.Errorf("content: %w", err)
	}
	return Content{Name: file.Name, Object: obj, Open: open}, nil
}

// deleteFile removes the file at version by the data package's delete
// protocol, its scope checked in the transaction that begins the delete. No
// row of the layer references a file, so no reference is removed before
// the delete. A file deleting already is the delete's retry, which
// converges at any version, so a delete is never refused as deleting.
func (s *store) deleteFile(ctx context.Context, organizationID, id string, version int64) error {
	return s.storage.Retire(ctx, s.db.DB, func(tx *sqlate.Tx) (string, error) {
		_, file, err := s.fileScope(ctx, tx, organizationID, id)
		return file.ID, err
	}, bfdata.AtVersion(version))
}

// moveFile moves the file into a directory within the same root, guarded
// by its version. The key is untouched, so no object moves. A move refused
// because the file's own delete began is the file deleting, and one
// refused because a directory it reaches is deleting is the directory's,
// as deletingFile tells them apart.
func (s *store) moveFile(ctx context.Context, organizationID, id string, version int64, m MoveFile) (Identity, error) {
	file, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		root, _, err := s.fileScope(ctx, tx, organizationID, id)
		if err != nil {
			return blobfs.File{}, err
		}
		dir, err := s.within(ctx, tx, root, m.DirectoryID)
		if err != nil {
			return blobfs.File{}, err
		}
		return s.storage.FS.Files.Move(ctx, tx, id, dir, m.Name, version)
	})
	if err != nil {
		return Identity{}, s.deletingFile(ctx, id, err)
	}
	return Identity{ID: file.ID, Version: file.Version}, nil
}

// seed is the layer's seed contribution, a file seed, to the data
// package's named states: the hierarchies a state carries under
// "documents". The seeder runs it after the seed's transaction commits,
// when the organizations already stand, since a file's write puts its
// object outside any transaction.
type seed struct{ store *store }

var _ data.FileSeed = seed{}

// Key names the hierarchies in a state file and in the seed's counts.
func (seed) Key() string { return "documents" }

// Verify prepares the layer's statements, the seed's path walk among
// them, and blobfs's, which the tree's writes run.
func (s seed) Verify(ctx context.Context) error {
	return errors.Join(s.store.Verify(ctx), s.store.storage.FS.Verify(ctx, s.store.db))
}

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

// seedTree ensures the organization's document root under the tree's root
// id, then its entries beneath it. The root is not counted: it is the
// hierarchy's anchor, not an entry.
func (s *store) seedTree(ctx context.Context, t seedTree) (int, error) {
	org, err := s.seedOrganization(ctx, s.db, t.Organization)
	if err != nil {
		return 0, err
	}
	root, err := s.ensureRoot(ctx, org, bfdata.WithID(t.Root))
	if err != nil {
		return 0, err
	}
	return s.seedEntries(ctx, root, t.Entries)
}

// seedEntries ensures each entry under the directory with id parent, in
// file order, and returns how many it created, the subtrees' included.
func (s *store) seedEntries(ctx context.Context, parent string, entries []seedEntry) (int, error) {
	created := 0
	for _, e := range entries {
		n, err := s.seedEntry(ctx, parent, e)
		created += n
		if err != nil {
			return created, fmt.Errorf("%s: %w", e.Name, err)
		}
	}
	return created, nil
}

// seedEntry ensures one entry by blobfs's insert-or-find under the entry's
// id: a directory on the pool, then its entries, and a file by the write
// protocol's retry-safe form, its content put and the row completed. An
// entry already there by name is left as it is and keeps its own id, the
// directory's contents still ensured beneath it: a file there under
// another id, a client's upload pending or complete, is never written
// over, since the write protocol reports it as a taken name. An entry
// whose id a row
// already carries under another name, one a client moved or renamed, is
// left where the client put it, a directory with its contents. So is an
// entry whose delete is under way, which the sweep finishes and the next
// seed writes again.
func (s *store) seedEntry(ctx context.Context, parent string, e seedEntry) (int, error) {
	st := s.storage
	isDir, err := e.directory()
	if err != nil {
		return 0, err
	}
	if isDir {
		dir, created, err := st.FS.Directories.Ensure(ctx, s.db, parent, e.Name, bfdata.WithID(e.ID))
		if errors.Is(err, blobfs.ErrIDTaken) {
			// A concurrent seed committed the directory between the
			// lookup and the insert; the lookup finds it now. An id a
			// moved directory holds is taken again.
			dir, created, err = st.FS.Directories.Ensure(ctx, s.db, parent, e.Name, bfdata.WithID(e.ID))
		}
		if errors.Is(err, blobfs.ErrIDTaken) || errors.Is(err, blobfs.ErrDeleting) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		n, err := s.seedEntries(ctx, dir.ID, e.Entries)
		if created {
			n++
		}
		return n, err
	}
	_, stored, err := st.Ensure(ctx, s.db.DB, e.ID, strings.NewReader(e.Content), int64(len(e.Content)), func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
		return st.FS.Files.Ensure(ctx, tx, st.Objects, parent, e.Name, e.ContentType, bfdata.WithID(e.ID))
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

// directoryOf presents a blobfs directory; the document root has no parent
// the API shows and is named "/", as blobfs names its own root.
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

// fileOf presents a blobfs file without its object key.
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

// present presents each of a listing's rows by of, or fails on the first
// row of refuses.
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

// errUnknownStatus is a blobfs status the layer does not name: one the
// library added that this layer has not mapped yet. No handler matches
// it, so the read that meets it answers 500, a server fault, rather than
// put the library's text on the wire.
var errUnknownStatus = errors.New("document: a blobfs status the layer does not name")

// directoryStatus names blobfs's directory status in the API's vocabulary,
// the layer's own, so a change to the library's names never changes the
// wire. A status the layer does not name is errUnknownStatus: a status
// blobfs adds is named here before the layer can present it.
func directoryStatus(s blobfs.DirectoryStatus) (DirectoryStatus, error) {
	switch s {
	case blobfs.DirectoryStatusActive:
		return DirectoryActive, nil
	case blobfs.DirectoryStatusDeleting:
		return DirectoryDeleting, nil
	}
	return "", fmt.Errorf("directory status %q: %w", s, errUnknownStatus)
}

// fileStatus names blobfs's file status in the API's vocabulary, as
// directoryStatus does.
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
