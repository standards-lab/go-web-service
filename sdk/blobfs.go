package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// WriteFile is blobfs's two-phase write, Store.WriteFile, with one change:
// the completion runs in a transaction on db, and completed runs in that
// transaction once Files.Complete has made the row available, so whatever
// it writes commits exactly when the file becomes available. A domain
// emits the event that reports the write there, through its recorder.
// It is bound for blobfs, as Store.WriteFile's completion hook; the task
// blobfs-write-protocol promotes it, and this staging goes then.
//
// It reproduces Store.WriteFile from the store's exported steps. begin
// runs Files.Create in one transaction on db, and its error, or its
// transaction's, is returned as it came. The put runs outside any
// transaction. A failed put, or a completion that fails for any other
// reason than the three below, completed's error among them, rolls the
// completion back and abandons the write: the pending row is removed at
// its version, by RemoveFileID with AtVersion, whose own failure is joined
// to the error and leaves the row to the sweep. A completion refused
// because a sweep reached the row first (blobfs.ErrDeleting) or removed it
// (blobfs.ErrNotFound) deletes the object just put and abandons nothing,
// and one that finds the version moved on or the row available
// (query.ErrVersionMismatch, blobfs.ErrInvalidTransition) abandons
// nothing: another writer moved or completed the row. completed never runs
// on a refused completion, so its writes never commit then.
//
// Store.WriteFile names whose delete refused a row begin returns deleting
// before the put, an unexported read; Files.Create always returns a
// pending row, so that refusal is the bare transition error here.
func WriteFile(ctx context.Context, fs *bfdata.Store, db *sqlate.DB, objects bfdata.ObjectStore, body io.Reader, size int64,
	begin func(*sqlate.Tx) (blobfs.File, error), completed func(*sqlate.Tx, blobfs.File) error,
) (_ blobfs.File, err error) {
	file, err := db.Transact(ctx, begin)
	if err != nil {
		return blobfs.File{}, err
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("sdk: write file %s: %w", file.ID, err)
		}
	}()
	if err := blobfs.Transition(file.Status, blobfs.StatusAvailable); err != nil {
		return blobfs.File{}, err
	}
	// The cleanup outlives ctx's cancellation, as Store.WriteFile's does: a
	// caller that hangs up mid-body cancels ctx, and the abandon and the
	// delete must still run rather than leave the row to the stale reclaim.
	cleanup := context.WithoutCancel(ctx)
	abandon := func(err error) (blobfs.File, error) {
		if rerr := fs.RemoveFileID(cleanup, db, objects, file.ID, bfdata.AtVersion(file.Version)); rerr != nil {
			return blobfs.File{}, errors.Join(err, fmt.Errorf("abandon: %w", rerr))
		}
		return blobfs.File{}, err
	}
	obj, err := objects.PutObject(ctx, file.Key, body, file.ContentType, size)
	if err != nil {
		return abandon(fmt.Errorf("put the object: %w", err))
	}
	// refused is the completion's own refusal, told apart from completed's
	// error and the transaction's, which abandon the write whatever they
	// wrap.
	var refused error
	done, err := db.Transact(ctx, func(tx *sqlate.Tx) (blobfs.File, error) {
		done, err := fs.Files.Complete(ctx, tx, file.ID, file.Version, obj)
		if err != nil {
			refused = err
			return blobfs.File{}, err
		}
		return done, completed(tx, done)
	})
	switch {
	case errors.Is(refused, blobfs.ErrDeleting), errors.Is(refused, blobfs.ErrNotFound):
		if derr := objects.DeleteObject(cleanup, file.Key); derr != nil {
			return blobfs.File{}, errors.Join(err, fmt.Errorf("delete the object: %w", derr))
		}
		return blobfs.File{}, err
	case errors.Is(refused, query.ErrVersionMismatch), errors.Is(refused, blobfs.ErrInvalidTransition):
		return blobfs.File{}, err
	case err != nil:
		return abandon(err)
	}
	return done, nil
}
