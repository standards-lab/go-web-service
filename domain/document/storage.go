package document

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
)

// directories and files are blobfs's two listings, lowered by listing.
func (s *store) directories() listing[blobfs.Directory] {
	return listing[blobfs.Directory]{List: s.storage.FS.Directories.List, Continue: s.storage.FS.Directories.Continue}
}

func (s *store) files() listing[blobfs.File] {
	return listing[blobfs.File]{List: s.storage.FS.Files.List, Continue: s.storage.FS.Files.Continue}
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
// insert, and the root it bound is read on the pool.
func (s *store) ensureRoot(ctx context.Context, organizationID string) (string, error) {
	root, err := s.documentRoot(ctx, s.db, organizationID)
	if !errors.Is(err, sql.ErrNoRows) {
		return root, err
	}
	root, err = s.db.Transact(ctx, func(tx *sqlate.Tx) (string, error) {
		if err := s.findOrganization(ctx, tx, organizationID); err != nil {
			return "", fmt.Errorf("organization %s: %w", organizationID, err)
		}
		dir, _, err := s.storage.FS.Directories.Ensure(ctx, tx, blobfs.RootID, organizationID)
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
	out := directoryOf(d, root)
	_, below, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	out.Path = "/" + below
	return out, nil
}

// listDirectories reads one page of the directory's child directories. An
// organization without a root lists nothing under the alias.
func (s *store) listDirectories(ctx context.Context, organizationID, id string, q web.Query) ([]Directory, web.Paging, error) {
	root, dir, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return rootless[Directory](id, err)
	}
	items, paging, err := s.directories().read(ctx, s.db, dir, q)
	out := make([]Directory, len(items))
	for i, d := range items {
		out[i] = directoryOf(d, root)
	}
	return out, paging, err
}

// listFiles reads one page of the directory's files, pending and
// available; a file whose delete has begun is hidden, as every listing
// hides deleting rows.
func (s *store) listFiles(ctx context.Context, organizationID, id string, q web.Query) ([]File, web.Paging, error) {
	_, dir, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return rootless[File](id, err)
	}
	items, paging, err := s.files().read(ctx, s.db, dir, q)
	out := make([]File, len(items))
	for i, f := range items {
		out[i] = fileOf(f)
	}
	return out, paging, err
}

// rootless answers a listing's scope failure: an organization without a
// root yet has an empty hierarchy, so the alias lists as an empty, counted
// page, while a specific id is not found. Any other failure is the
// request's.
func rootless[T any](id string, err error) ([]T, web.Paging, error) {
	if id == RootAlias && errors.Is(err, sql.ErrNoRows) {
		return nil, web.Paging{}, nil
	}
	return nil, web.Paging{}, err
}

// deleteDirectory removes the empty directory at version. Removing the
// root removes its owner row with it.
func (s *store) deleteDirectory(ctx context.Context, organizationID, id string, version int64) error {
	root, id, err := s.scope(ctx, s.db, organizationID, id)
	if err != nil {
		return err
	}
	return s.remove(ctx, id, id == root, version)
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

// remove removes one empty directory at version. The root's owner row
// goes in the same transaction, since its foreign key refuses the
// directory's removal while it stands.
func (s *store) remove(ctx context.Context, id string, root bool, version int64) error {
	fs := s.storage.FS
	at := bfdata.AtVersion(version)
	if !root {
		return fs.Directories.Delete(ctx, s.db, id, at)
	}
	_, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (struct{}, error) {
		if err := s.unbind(ctx, tx, id); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, fs.Directories.Delete(ctx, tx, id, at)
	})
	return err
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

// putFile stores the upload as a new file named name in the directory,
// ensuring the root when the directory is its alias. The write's steps
// each run on their own session: the scope check and the pending row
// commit together before any byte is stored, the put runs outside any
// transaction, and the completion on the pool. A put or a completion that
// fails retires the pending row, so its name is free for a retry; one whose
// retire fails too leaves the row pending, blobfs's state for a sweep. A
// completion refused because the row's delete began or the sweep removed
// it, the mark of a branch that raced the write, deletes the object just
// put under the key the write holds, since a sweep that ran before the put
// landed cannot have deleted it; the row is the sweep's.
func (s *store) putFile(ctx context.Context, organizationID, directoryID, name string, u web.Upload) (Identity, error) {
	fs, objects := s.storage.FS, s.storage.Objects
	directoryID, err := s.writable(ctx, organizationID, directoryID)
	if err != nil {
		return Identity{}, err
	}
	file, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		_, dir, err := s.scope(ctx, tx, organizationID, directoryID)
		if err != nil {
			return blobfs.File{}, err
		}
		return fs.Files.Create(ctx, tx, objects, dir, name, u.ContentType)
	})
	if err != nil {
		return Identity{}, err
	}
	abandon := func(err error) (Identity, error) {
		return Identity{}, errors.Join(err, s.retire(ctx, func(*sqlate.Tx) (string, error) { return file.ID, nil }))
	}
	obj, err := objects.Put(ctx, file.Key, u.Body, u.ContentType, u.Size)
	if err != nil {
		return abandon(err)
	}
	done, err := fs.Files.Complete(ctx, s.db, file.ID, file.Version, obj)
	switch {
	case errors.Is(err, blobfs.ErrDeleting), errors.Is(err, blobfs.ErrNotFound):
		return Identity{}, errors.Join(err, objects.Delete(ctx, file.Key))
	case err != nil:
		return abandon(err)
	}
	file = done
	return Identity{ID: file.ID, Version: file.Version}, nil
}

// file reads the file's metadata.
func (s *store) file(ctx context.Context, organizationID, id string) (File, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	return fileOf(file), err
}

// content reads the file for its download. Only an available file is
// served; any other is not found, as an absent one is.
func (s *store) content(ctx context.Context, organizationID, id string) (Content, error) {
	_, file, err := s.fileScope(ctx, s.db, organizationID, id)
	if err != nil {
		return Content{}, err
	}
	if file.Status != blobfs.StatusAvailable || file.Size == nil || file.ETag == nil {
		return Content{}, fmt.Errorf("content of file %s, which is %s: %w", file.ID, file.Status, blobfs.ErrNotFound)
	}
	return Content{
		Name:   file.Name,
		Object: web.Object{ContentType: file.ContentType, Size: *file.Size, ETag: *file.ETag, ModifiedAt: file.UpdatedAt},
		Open:   func() (io.ReadCloser, error) { return s.storage.Objects.Open(ctx, file.Key) },
	}, nil
}

// deleteFile removes the file at version by the delete protocol, its
// scope checked in the transaction that begins the delete. A file deleting
// already is the delete's retry, which converges at any version.
func (s *store) deleteFile(ctx context.Context, organizationID, id string, version int64) error {
	return s.retire(ctx, func(tx *sqlate.Tx) (string, error) {
		_, file, err := s.fileScope(ctx, tx, organizationID, id)
		return file.ID, err
	}, bfdata.AtVersion(version))
}

// moveFile moves the file into a directory within the same root, guarded
// by its version. The key is untouched, so no object moves.
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
	return Identity{ID: file.ID, Version: file.Version}, err
}

// retire runs the delete protocol over the file pick names in its
// transaction: blobfs's delete begun in that transaction, under the
// version guard when opts carries one, then the object deleted, then the
// row purged on the pool. No row of the layer references a file, so no
// reference check precedes the delete. Every step converges on a retry.
func (s *store) retire(ctx context.Context, pick func(*sqlate.Tx) (string, error), opts ...bfdata.VersionOption) error {
	fs := s.storage.FS
	file, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		id, err := pick(tx)
		if err != nil {
			return blobfs.File{}, err
		}
		return fs.Files.Delete(ctx, tx, id, opts...)
	})
	if err != nil {
		return err
	}
	if err := s.storage.Objects.Delete(ctx, file.Key); err != nil {
		return err
	}
	return fs.Files.Purge(ctx, s.db, file.ID)
}

// directoryOf presents a blobfs directory; the document root has no parent
// the API shows and is named "/", as blobfs names its own root.
func directoryOf(d blobfs.Directory, root string) Directory {
	out := Directory{ID: d.ID, ParentID: d.ParentID, Name: d.Name, Status: d.Status, Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	if d.ID == root {
		out.ParentID, out.Name = nil, "/"
	}
	return out
}

// fileOf presents a blobfs file without its object key.
func fileOf(f blobfs.File) File {
	return File{
		ID: f.ID, DirectoryID: f.DirectoryID, Name: f.Name, Status: f.Status, Size: f.Size,
		ContentType: f.ContentType, Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}
}
