package data

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// The file protocols every domain runs over Storage, staged here for
// promotion to blobfs: the two-phase write with its retry-safe form, the
// two-phase delete, and the read of an available file. Each takes the pool
// it runs on as blobfs's sweep does, and knows no domain: a domain's scope
// checks and its own rows enter through the callback each protocol runs in
// its first transaction.

// Write is blobfs's two-phase write of the file begin creates, with body
// of size bytes as its object. It runs begin in one transaction on db,
// where the domain checks its scope and calls Files.Create or Files.Ensure,
// beside any row of its own that references nothing of blobfs's, so the
// pending row commits before any byte is stored. It then puts body under
// the pending row's key outside any transaction, in the content type the
// row declares, and completes the row on the pool, returning it available.
//
// A put or a completion that fails abandons the write through Retire, so
// the row's name is free for a retry; an abandon that fails too leaves the
// row pending or deleting, a stale row blobfs's sweep reclaims, which is
// why begin must insert no row that references the file: a reference
// would refuse the reclaim's purge. A completion refused with
// blobfs.ErrDeleting or blobfs.ErrNotFound, a reclaim or a branch's sweep
// that reached the row before the put landed, deletes the object just put
// under the key the write holds, since that sweep could not have deleted
// it, and leaves the row to the sweep: the writer rule.
func (s *Storage) Write(ctx context.Context, db *sqlate.DB, body io.Reader, size int64, begin func(*sqlate.Tx) (blobfs.File, error)) (blobfs.File, error) {
	file, err := db.Transact(ctx, begin)
	if err != nil {
		return blobfs.File{}, err
	}
	return s.store(ctx, db, body, size, file)
}

// Ensure is the write protocol's retry-safe form, the one a seed runs for a
// file it names by a fixed id: begin runs in one transaction on db, where
// the domain calls Files.Ensure with that id, and reports what Ensure did.
// A row it created, or a pending row an earlier write left, is written as
// Write writes it, the object put under the row's key and the row
// completed, and stored reports true. An available row is returned as it
// stands, nothing put; a deleting one is blobfs.ErrDeleting, since its
// delete is under way.
//
// Two writers, two processes seeding at once, may race for one row. The
// loser of the insert has its transaction aborted by the violation, so
// begin runs once more in a fresh one, which finds the winner's row. Once
// the pending row commits, the other writer finds it and resumes it, so
// the two may share one row. The write is shaped for that: its abandon
// begins the delete only at the pending version it holds, so it never
// retires a row the other writer completed, which an image may reference
// by then; and a completion the other writer won, a stale version or a
// row available already, is no failure, the row read back and returned as
// found. The two puts store the same bytes under the same key, all or
// nothing, so the object is whole whichever lands last.
//
// The key is the row's id and name, so a file written again under its
// fixed id after a reset, which reverts blobfs's tables but not the
// container, puts its object under the key the earlier write used. The put
// replaces the object there whole, so the write completes over whatever
// the container still held.
func (s *Storage) Ensure(ctx context.Context, db *sqlate.DB, body io.Reader, size int64, begin func(*sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error)) (file blobfs.File, stored bool, err error) {
	type ensured struct {
		file    blobfs.File
		outcome bfdata.WriteOutcome
	}
	first := func(tx *sqlate.Tx) (ensured, error) {
		file, outcome, err := begin(tx)
		return ensured{file, outcome}, err
	}
	e, err := db.Transact(ctx, first)
	if errors.Is(err, blobfs.ErrNameTaken) || errors.Is(err, blobfs.ErrIDTaken) {
		// A concurrent writer committed the row between the lookup and
		// the insert, whose failure aborted the transaction; a fresh one
		// finds it. An id a row holds under another name fails again.
		e, err = db.Transact(ctx, first)
	}
	switch {
	case err != nil:
		return blobfs.File{}, false, err
	case e.outcome == bfdata.WritePresent && e.file.Status != blobfs.StatusAvailable:
		return blobfs.File{}, false, fmt.Errorf("file %s is %s: %w", e.file.ID, e.file.Status, blobfs.ErrDeleting)
	case e.outcome == bfdata.WritePresent:
		return e.file, false, nil
	}
	file, err = s.store(ctx, db, body, size, e.file, bfdata.AtVersion(e.file.Version))
	if !errors.Is(err, query.ErrVersionMismatch) && !errors.Is(err, blobfs.ErrInvalidTransition) {
		return file, err == nil, err
	}
	// Another writer completed the row first.
	found, ferr := s.FS.Files.Find(ctx, db, e.file.ID)
	if ferr != nil || found.Status != blobfs.StatusAvailable {
		return blobfs.File{}, false, errors.Join(err, ferr)
	}
	return found, false, nil
}

// store is the write protocol after its first transaction: the put of
// body under the pending file's key, outside any transaction, then the
// completion on the pool, with the abandon and the writer rule Write
// describes. guard is the abandon's version guard: none for a row the
// writer holds alone, the pending version for a row Ensure may share. A
// guarded write abandons nothing when its completion finds the version
// moved on or the row available, since another writer completed it.
func (s *Storage) store(ctx context.Context, db *sqlate.DB, body io.Reader, size int64, file blobfs.File, guard ...bfdata.VersionOption) (blobfs.File, error) {
	// The cleanup outlives the request's cancellation: a client that hangs
	// up mid-body cancels ctx, and the abandon and the writer rule's delete
	// must still run rather than leave the row to the stale reclaim.
	cleanup := context.WithoutCancel(ctx)
	abandon := func(err error) (blobfs.File, error) {
		return blobfs.File{}, errors.Join(err, s.Retire(cleanup, db, func(*sqlate.Tx) (string, error) { return file.ID, nil }, guard...))
	}
	obj, err := s.Objects.Put(ctx, file.Key, body, file.ContentType, size)
	if err != nil {
		return abandon(err)
	}
	done, err := s.FS.Files.Complete(ctx, db, file.ID, file.Version, obj)
	switch {
	case errors.Is(err, blobfs.ErrDeleting), errors.Is(err, blobfs.ErrNotFound):
		return blobfs.File{}, errors.Join(err, s.Objects.Delete(cleanup, file.Key))
	case len(guard) > 0 && (errors.Is(err, query.ErrVersionMismatch) || errors.Is(err, blobfs.ErrInvalidTransition)):
		return blobfs.File{}, err
	case err != nil:
		return abandon(err)
	}
	return done, nil
}

// Retire is blobfs's two-phase delete of the file pick names. It runs pick
// in one transaction on db, where the domain checks its scope and removes
// its own references to the file, then begins blobfs's delete in that
// transaction, under the version guard when opts carries one, then runs
// Purge. Every step converges on a retry, and a delete begun at any version
// is the retry of one already begun.
func (s *Storage) Retire(ctx context.Context, db *sqlate.DB, pick func(*sqlate.Tx) (string, error), opts ...bfdata.VersionOption) error {
	file, err := db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		id, err := pick(tx)
		if err != nil {
			return blobfs.File{}, err
		}
		return s.FS.Files.Delete(ctx, tx, id, opts...)
	})
	if err != nil {
		return err
	}
	return s.Purge(ctx, db, file)
}

// Purge is the delete protocol after its first step, for a file whose
// Files.Delete a domain ran in a transaction of its own: the object under
// the deleting file's key, then the row, on db. It is Retire's tail, and
// converges on a retry as Retire does; a purge that never runs leaves a
// deleting row the sweep's stale reclaim finishes.
func (s *Storage) Purge(ctx context.Context, db *sqlate.DB, file blobfs.File) error {
	if err := s.Objects.Delete(ctx, file.Key); err != nil {
		return err
	}
	return s.FS.Files.Purge(ctx, db, file.ID)
}

// Serve describes an available file as a download serves it: the object's
// description, which answers a revalidation alone, and the open that
// streams its bytes under ctx and runs only when they are sent. Any other
// file is blobfs.ErrNotFound, as an absent one is, so a pending upload or
// a file whose delete has begun is never served.
func (s *Storage) Serve(ctx context.Context, file blobfs.File) (web.Object, func() (io.ReadCloser, error), error) {
	if file.Status != blobfs.StatusAvailable || file.Size == nil || file.ETag == nil {
		return web.Object{}, nil, fmt.Errorf("file %s is %s: %w", file.ID, file.Status, blobfs.ErrNotFound)
	}
	obj := web.Object{ContentType: file.ContentType, Size: *file.Size, ETag: *file.ETag, ModifiedAt: file.UpdatedAt}
	return obj, func() (io.ReadCloser, error) { return s.Objects.Open(ctx, file.Key) }, nil
}
