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
	"github.com/standards-lab/go-web-service/sdk"
)

// listing reads one page of blobfs's listing l, Directories' or Files',
// of the directory with id, as the request addressed it, through the data
// package's lowering. Deleting rows are hidden, and the listing of a
// directory in a branch being deleted is not found: blobfs refuses it as
// blobfs.ErrDeleting, which the layer reports as the missing directory
// rather than a conflict, since a mark is never undone, so nothing a
// client sends makes the directory listable again.
func listing[T any](ctx context.Context, sess sqlate.Session, l bfdata.Listing[T], id string, q web.Query) ([]T, web.Paging, error) {
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
// The transaction that creates the root emits DirectoryCreated for it, so
// the root's event precedes the write's own, which commits after it. A
// concurrent first write that bound the root first fails this one's
// insert, rolling back its event with it, and the root it bound is read on
// the pool. opts reach blobfs's insert, the seed's fixed id among them, so
// a seed that creates a root emits its event too.
func (s *store) ensureRoot(ctx context.Context, organizationID string, opts ...bfdata.CreateOption) (string, error) {
	root, err := s.documentRoot(ctx, s.db, organizationID)
	if !errors.Is(err, sql.ErrNoRows) {
		return root, err
	}
	root, err = s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (string, error) {
		if err := s.organizationExists(ctx, tx, organizationID); err != nil {
			return "", fmt.Errorf("organization %s: %w", organizationID, err)
		}
		dir, created, err := s.storage.FS.Directories.Ensure(ctx, tx, blobfs.RootID, organizationID, opts...)
		if err != nil {
			return "", err
		}
		if err := s.bind(ctx, tx, organizationID, dir.ID); err != nil {
			return "", err
		}
		// A directory found rather than created is no root this write
		// made: a found one either belongs to the write that won the
		// race, whose bind refuses this one's, or was made before.
		if created {
			raiseDirectory(q, DirectoryCreated, organizationID, dir.ID, dir)
		}
		return dir.ID, nil
	}))
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, sqlate.ErrUniqueViolation) {
		return s.documentRoot(ctx, s.db, organizationID)
	}
	return root, err
}

// createDirectory creates a directory under the parent, within the root,
// ensuring the root when the parent is its alias, and emits
// DirectoryCreated in the create's transaction, its data the row the
// insert returned. A refused create, a parent out of scope or a taken
// name, commits no event.
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
		dir, err := s.storage.FS.Directories.Create(ctx, tx, parent, c.Name)
		if err != nil {
			return blobfs.Directory{}, err
		}
		raiseDirectory(q, DirectoryCreated, organizationID, root, dir)
		return dir, nil
	}))
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

// directory reads the directory with its path from the root. The scope
// check, the directory, and its path run in one transaction, so the three
// reads see one tree. blobfs's path runs from its own root, and its first
// segment is the document root.
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
	})
}

// listDirectories reads one page of the directory's child directories. An
// organization without a root lists nothing under the alias.
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

// listFiles reads one page of the directory's files, pending and
// available; a file whose delete has begun is hidden, as every listing
// hides deleting rows.
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

// listScope is scope for a listing. An organization without a root yet
// has an empty hierarchy: once the organization is read, the alias
// resolves to an id minted for the read, which no row holds, so blobfs
// lists it as an empty directory. The alias thus runs the listing itself:
// it refuses what a real root's listing refuses (a sort, filter, or cursor
// the read does not accept) and reports the paging an empty root's would.
// A nonexistent organization is the missing row, as every other route
// answers it, and a specific id is not found.
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

// deleteDirectory removes the empty directory at version in one
// transaction with its scope check, and DirectoryDeleted, at the version
// the guarded delete matched, the one the row was removed at. Removing the
// root removes its owner row with it, through the owner row's cascading
// foreign key, so the organization has no root until its next write
// ensures a new one. A refused delete commits no event.
func (s *store) deleteDirectory(ctx context.Context, organizationID, id string, version int64) error {
	_, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (struct{}, error) {
		_, id, err := s.scope(ctx, tx, organizationID, id)
		if err != nil {
			return struct{}{}, err
		}
		if err := s.storage.FS.Directories.Delete(ctx, tx, id, bfdata.AtVersion(version)); err != nil {
			return struct{}{}, err
		}
		DirectoryDeleted.Raise(q, id, DirectoryDeletedData{DirectoryID: id, OrganizationID: organizationID, Version: version})
		return struct{}{}, nil
	}))
	return err
}

// markBranch begins the delete of the directory with everything beneath
// it: blobfs's mark, the directory at version, in one transaction with the
// scope check and DirectoryDeleted for the directory alone, and returns
// the directory's id. The sweep removes the branch after the commit and
// emits nothing. A directory deleting already is the mark's retry, which
// converges at any version and emits nothing, the first mark having
// emitted the directory's event.
//
// Whether the mark is the first is the directory's own status, read in the
// mark's transaction before the mark, not blobfs's count: a retry descends
// through the deleting branch and counts a straggler a create raced into
// it after the first mark, which is no new delete of the directory. The
// read follows blobfs's tree lock, which the mark takes again, so on the
// Postgres engine the service runs, whose lock is held to the end of the
// transaction and re-entrant, no other mark commits between the read and
// the mark: a directory read active is the first mark's. The count guard
// serves the baseline variant alone, whose lock is a no-op: there a
// concurrent first mark can commit between the read and the mark, which
// then counts nothing and leaves the event to the first mark. A straggler
// that mark races in beside it still counts, and only the lock closes that.
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
		marked, err := s.storage.FS.Directories.MarkDeleting(ctx, tx, id, bfdata.AtVersion(version))
		if err != nil || anchor.Status != blobfs.DirectoryStatusActive || marked.Directories == 0 {
			return id, err
		}
		return id, s.raiseMarked(ctx, tx, q, organizationID, id)
	}))
}

// moveDirectory moves the directory under a new parent within the same
// root, under blobfs's tree lock and cycle check and the version guard, and
// emits DirectoryMoved in the move's transaction, its data the row the
// update returned. The root itself stays where it is. A refused move, a
// cycle, a stale version, or a taken name, commits no event.
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
		dir, err := s.storage.FS.Directories.Move(ctx, tx, id, parent, m.Name, version)
		if err != nil {
			return blobfs.Directory{}, err
		}
		raiseDirectory(q, DirectoryMoved, organizationID, root, dir)
		return dir, nil
	}))
	return Identity{ID: dir.ID, Version: dir.Version}, err
}

// uploadFile stores the upload as a new file named name in the directory,
// ensuring the root when the directory is its alias, by blobfs's two-phase
// write as the sdk package stages it, whose first transaction runs the
// scope check with the pending row's insert and whose completion's
// transaction emits FileUploaded. A put or completion that fails, the
// event's write among them, abandons the row and commits no event, and the
// sweep finishes one whose abandon fails too. A completion refused as
// deleting (a blobfs.DeletingError, which names whose delete it was)
// deletes the object just put.
func (s *store) uploadFile(ctx context.Context, organizationID, directoryID, name string, u web.Upload) (Identity, error) {
	st := s.storage
	directoryID, err := s.writable(ctx, organizationID, directoryID)
	if err != nil {
		return Identity{}, err
	}
	file, err := sdk.WriteFile(ctx, st.FS, s.db.DB, st.Objects, u.Body, u.Size, func(tx *sqlate.Tx) (blobfs.File, error) {
		_, dir, err := s.scope(ctx, tx, organizationID, directoryID)
		if err != nil {
			return blobfs.File{}, err
		}
		return st.FS.Files.Create(ctx, tx, st.Objects, dir, name, u.ContentType)
	}, func(tx *sqlate.Tx, f blobfs.File) error {
		return s.emitUploaded(ctx, tx, organizationID, f)
	})
	return Identity{ID: file.ID, Version: file.Version}, err
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
	dl, err := s.storage.Serve(ctx, file)
	if err != nil {
		return Content{}, fmt.Errorf("content: %w", err)
	}
	return Content{Name: file.Name, Object: dl.Object, Open: dl.Open}, nil
}

// deleteFile removes the file at version by blobfs's two-phase delete as
// the sdk package stages it, its scope checked in the transaction that
// begins the delete, which emits FileDeleted at the version that left the
// file deleting. No row of the layer references a file, so no reference is
// removed before the delete. A file deleting already is the delete's
// retry, which converges at any version, so a delete is never refused as
// deleting; the retry emits nothing and finishes the purge, and the purge
// emits nothing. A refused delete, out of scope or at a stale version,
// commits no event.
func (s *store) deleteFile(ctx context.Context, organizationID, id string, version int64) error {
	st := s.storage
	return sdk.RemoveFile(ctx, st.FS, s.db.DB, st.Objects, func(tx *sqlate.Tx) (string, error) {
		_, file, err := s.fileScope(ctx, tx, organizationID, id)
		return file.ID, err
	}, func(tx *sqlate.Tx, f blobfs.File) error {
		return s.emitDeleted(ctx, tx, organizationID, f)
	}, bfdata.AtVersion(version))
}

// moveFile moves the file into a directory within the same root, guarded
// by its version, and emits FileMoved in the move's transaction, its data
// the row the update returned. The key is untouched, so no object moves. A
// move refused as deleting is blobfs's blobfs.DeletingError, the file's
// own when its delete began and the directory's when a directory it
// reaches is deleting, which data.Status tells apart. A refused move
// commits no event.
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
		file, err := s.storage.FS.Files.Move(ctx, tx, id, dir, m.Name, version)
		if err != nil {
			return blobfs.File{}, err
		}
		FileMoved.Raise(q, file.ID, fileData(organizationID, file))
		return file, nil
	}))
	return Identity{ID: file.ID, Version: file.Version}, err
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
// hierarchy's anchor, not an entry. Each entry the seed creates commits the
// event its command's creation commits, with the same data, in the
// transaction that creates it: the root's DirectoryCreated, as ensureRoot's
// every creation does, each directory's DirectoryCreated, and each file's
// FileUploaded. An entry found already there commits none.
func (s *store) seedTree(ctx context.Context, t seedTree) (int, error) {
	org, err := s.seedOrganization(ctx, s.db, t.Organization)
	if err != nil {
		return 0, err
	}
	root, err := s.ensureRoot(ctx, org, bfdata.WithID(t.Root))
	if err != nil {
		return 0, err
	}
	return s.seedEntries(ctx, org, root, root, t.Entries)
}

// seedEntries ensures each entry under the directory with id parent, in
// the organization's hierarchy under root, in file order, and returns how
// many it created, the subtrees' included.
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

// seedEntry ensures one entry under the entry's id: a directory by
// seedDirectory, then its entries, and a file by blobfs's retry-safe write
// as the sdk package stages it, sdk.EnsureFile, whose completion's
// transaction emits FileUploaded when this call completes the file, as an
// upload's does. An entry the seed does not own is left as it stands and
// counts nothing: one there by name under another id (its directory's
// contents are still ensured), one whose id a row carries under another
// name, and one whose delete is under way.
func (s *store) seedEntry(ctx context.Context, organizationID, root, parent string, e seedEntry) (int, error) {
	st := s.storage
	isDir, err := e.directory()
	if err != nil {
		return 0, err
	}
	if isDir {
		dir, created, err := s.seedDirectory(ctx, organizationID, root, parent, e)
		// ErrIDTaken here is an id a moved directory holds: the rerun after
		// a concurrent seed's insert finds that seed's directory by name.
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
	_, stored, err := sdk.EnsureFile(ctx, st.FS, s.db.DB, st.Objects, e.ID, strings.NewReader(e.Content), int64(len(e.Content)),
		func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
			return st.FS.Files.Ensure(ctx, tx, st.Objects, parent, e.Name, e.ContentType, bfdata.WithID(e.ID))
		}, func(tx *sqlate.Tx, f blobfs.File) error {
			return s.emitUploaded(ctx, tx, organizationID, f)
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

// seedDirectory ensures the entry's directory under parent by blobfs's
// insert-or-find in one transaction, which emits DirectoryCreated with the
// data a create's carries when the insert created the directory, and
// nothing when it found one. A concurrent seed's insert under the name or
// the id aborts the transaction, which blobfs cannot recover inside it, so
// the violation reruns it once in a fresh one, which finds that seed's
// directory and emits nothing; an id a moved directory holds fails again.
func (s *store) seedDirectory(ctx context.Context, organizationID, root, parent string, e seedEntry) (blobfs.Directory, bool, error) {
	type ensured struct {
		dir     blobfs.Directory
		created bool
	}
	ensure := s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (ensured, error) {
		dir, created, err := s.storage.FS.Directories.Ensure(ctx, tx, parent, e.Name, bfdata.WithID(e.ID))
		if err != nil {
			return ensured{}, err
		}
		if created {
			raiseDirectory(q, DirectoryCreated, organizationID, root, dir)
		}
		return ensured{dir, created}, nil
	})
	r, err := s.db.Transact(ctx, ensure)
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, blobfs.ErrIDTaken) {
		r, err = s.db.Transact(ctx, ensure)
	}
	return r.dir, r.created, err
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
