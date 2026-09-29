package data

import (
	"context"
	"io"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-storage"
)

// Storage is the object storage infrastructure as the domains see it:
// blobfs's store, whose tables hold a row for every stored file and a tree
// of directories over them, run through the same session as [Database],
// and the object store the rows' keys name. The composition root installs
// the engine and starts the object store. A domain runs the file protocols
// that span the two, blobfs's two-phase write and delete, from its storage
// translation file as FS's own methods over Objects, with its scope checks
// and its own rows as their callbacks, and sequences the rest, the moves
// and its directories, over blobfs's steps directly. The read of an
// available file, which answers in the web SDK's terms, is Storage's own
// (Serve, in protocol.go).
type Storage struct {
	FS      *bfdata.Store
	Objects *Objects
}

// NewStorage groups blobfs's store with the object store it keys into. No
// I/O happens here.
func NewStorage(fs *bfdata.Store, objects *storage.Store) *Storage {
	return &Storage{FS: fs, Objects: &Objects{store: objects}}
}

// Objects is the adapter between blobfs and a started store, the one place
// the domains' file operations reach the object-store library, so no domain
// imports it. It is the key validator blobfs's writes take as their first
// step's argument, the object store blobfs's write and delete protocols and
// its sweep call, and the open Serve streams a download through.
//
// A missing container is go-storage's storage.ErrContainerNotFound on
// every operation, which never matches storage.ErrNotFound, so a put, an
// open, or a delete against it is refused as the store's fault rather
// than read as an absent object; blobfs leaves the row for a later pass.
type Objects struct {
	store *storage.Store
}

var _ bfdata.ObjectStore = (*Objects)(nil)

// ValidateKey checks key against the store's own key rule, as blobfs's
// write asks before it inserts a row.
func (o *Objects) ValidateKey(key string) error {
	return o.store.Capabilities().ValidateKey(key)
}

// PutObject stores body under key, all or nothing, and reports the object
// as blobfs's completion records it. The content type is the one the
// caller declared, since a store's own report of it may differ once
// written.
func (o *Objects) PutObject(ctx context.Context, key string, body io.Reader, contentType string, size int64) (blobfs.Object, error) {
	obj, err := o.store.Put(ctx, key, body, storage.PutOptions{ContentType: contentType, Size: size})
	if err != nil {
		return blobfs.Object{}, err
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

// DeleteObject removes the object under key, the step blobfs's delete, its
// write's abandon, and its sweep run between their transactions. It is
// idempotent as blobfs requires: go-storage's Delete of a missing key
// succeeds, on every provider.
func (o *Objects) DeleteObject(ctx context.Context, key string) error {
	return o.store.Delete(ctx, key)
}
