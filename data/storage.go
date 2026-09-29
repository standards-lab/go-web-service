package data

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-storage"
)

// ErrContainerGone is an object operation that found the configured
// container missing: the target itself is gone, so the object may well
// exist somewhere else, and the step is refused rather than read as done.
var ErrContainerGone = errors.New("data: the object store's container is missing")

// Storage is the object storage infrastructure as the domains see it:
// blobfs's store, whose tables hold a row for every stored file and a tree
// of directories over them, run through the same session as [Database],
// and the object store the rows' keys name. The composition root installs
// the engine and starts the object store. The file protocols that span the
// two, the write, the delete, and the read of an available file, are
// Storage's own methods (protocol.go); a domain runs them from its storage
// translation file with its scope checks and its own rows as their
// callbacks, and sequences the rest, the moves and its directories, over
// blobfs's steps directly.
type Storage struct {
	FS      *bfdata.Store
	Objects *Objects
}

// NewStorage groups blobfs's store with the object store it keys into. No
// I/O happens here.
func NewStorage(fs *bfdata.Store, objects *storage.Store) *Storage {
	return &Storage{FS: fs, Objects: &Objects{store: objects}}
}

// Objects is the adapter between blobfs's protocol steps and a started
// store, the one place the domains' file operations reach the object-store
// library, so no domain imports it. It is the key validator blobfs's
// writes take as their first step's argument, the put, open, and delete
// the steps between them run, and the object deleter blobfs's sweep calls.
type Objects struct {
	store *storage.Store
}

var _ bfdata.ObjectDeleter = (*Objects)(nil)

// ValidateKey checks key against the store's own key rule, as blobfs's
// write asks before it inserts a row.
func (o *Objects) ValidateKey(key string) error {
	return o.store.Capabilities().ValidateKey(key)
}

// Put stores body under key, all or nothing, and reports the object as
// blobfs's complete step takes it. The content type is the one the caller
// declared, since a store's own report of it may differ once written.
func (o *Objects) Put(ctx context.Context, key string, body io.Reader, contentType string, size int64) (blobfs.Object, error) {
	obj, err := o.store.Put(ctx, key, body, storage.PutOptions{ContentType: contentType, Size: size})
	if err != nil {
		return blobfs.Object{}, o.classify(err)
	}
	return blobfs.Object{Size: obj.Size, ContentType: contentType, ETag: obj.ETag}, nil
}

// Open streams the object under key; the caller closes it.
func (o *Objects) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	blob, err := o.store.Get(ctx, key, storage.GetOptions{})
	if err != nil {
		return nil, err
	}
	return blob.Body, nil
}

// Delete removes the object under key, the delete protocol's middle step.
// A missing object is success, as blobfs's own delete step treats it; a
// missing container is ErrContainerGone.
func (o *Objects) Delete(ctx context.Context, key string) error {
	return o.classify(o.store.Delete(ctx, key))
}

// DeleteObject is Delete under the name blobfs's sweep calls it by, so the
// adapter is the sweep's bfdata.ObjectDeleter. It is idempotent as the
// sweep requires: go-storage's Delete of a missing key succeeds, on every
// provider. A missing container is ErrContainerGone, which the sweep
// takes as a refusal, leaving the file deleting for a later pass.
func (o *Objects) DeleteObject(ctx context.Context, key string) error {
	return o.Delete(ctx, key)
}

// classify separates a missing container from the store's other errors. A
// write or delete that reports storage.ErrNotFound cannot have missed the
// object, so the container is what is gone.
func (o *Objects) classify(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: %w", ErrContainerGone, err)
	}
	return err
}
