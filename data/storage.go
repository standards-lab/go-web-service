package data

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
)

// Storage is the object storage as the domains see it: blobfs's store,
// whose rows run through the same session as [Database], and Objects, the
// object store the rows' keys name. A domain runs blobfs's file protocols
// (FS.WriteFile, FS.EnsureFile, FS.RemoveFile, FS.RemoveFileID,
// FS.PurgeFile) over Objects, with its scope checks and its own rows in
// their callbacks, and runs blobfs's steps directly for the rest. Serve
// reads an available file in the web SDK's terms.
type Storage struct {
	FS      *bfdata.Store
	Objects *Objects
}

// NewStorage groups blobfs's store with the object store it keys into and
// records the store on db, whose session runs its rows, for
// [Seeder.Verify] to check its statements. No I/O happens here.
func NewStorage(db *Database, fs *bfdata.Store, objects *storage.Store) *Storage {
	db.record(fs)
	return &Storage{FS: fs, Objects: &Objects{store: objects}}
}

// Download is an available file as a download serves it: the object's
// description, which answers a revalidation alone, and the open that
// streams its bytes, run only when they are sent.
type Download struct {
	Object web.Object
	Open   func() (io.ReadCloser, error)
}

// Serve describes an available file as a download, its open streaming
// under ctx. Any other file is blobfs.ErrNotFound, as an absent one is, so
// a pending upload or a file whose delete has begun is never served.
func (s *Storage) Serve(ctx context.Context, file blobfs.File) (Download, error) {
	if file.Status != blobfs.StatusAvailable || file.Size == nil || file.ETag == nil {
		return Download{}, fmt.Errorf("file %s is %s: %w", file.ID, file.Status, blobfs.ErrNotFound)
	}
	return Download{
		Object: web.Object{ContentType: file.ContentType, Size: *file.Size, ETag: *file.ETag, ModifiedAt: file.UpdatedAt},
		Open:   func() (io.ReadCloser, error) { return s.Objects.Open(ctx, file.Key) },
	}, nil
}

// ErrBodyRead reports an upload whose request body failed while its object
// was stored: a client that sent fewer bytes than it declared, or hung up.
// Status answers it 400, so a truncated upload is never read as the
// database's lost connection, which fails with the same io.ErrUnexpectedEOF.
var ErrBodyRead = errors.New("the request body could not be read")

// ErrBodyTimeout reports an upload whose client was too slow: its request
// body did not arrive before the connection's read deadline, which the
// route's transfer sets from the body's size and the slowest pace a client
// is allowed. Status answers it 408.
var ErrBodyTimeout = errors.New("the request body did not arrive in time")

// Objects adapts a started store to blobfs's ObjectStore and supplies the
// open Serve streams through. It is the one place the domains' file
// operations reach the object-store library, so no domain imports it.
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
//
// A put that fails because its body failed is the client's fault, and the
// body's error takes precedence over whatever the provider reported, since
// a provider fails a put whose body fails. The put returns ErrBodyTimeout
// when the body's read deadline passed (os.ErrDeadlineExceeded, or any
// net.Error whose Timeout is true), and ErrBodyRead when the body ended
// short of its declared size or broke off (io.ErrUnexpectedEOF, a reset
// connection, a malformed chunk). A store that stalls never makes the
// body's deadline pass: azureblob reads up to block_size × concurrency of
// the body ahead of the store, more than max_object_size, so the body is
// read whole, or fails on its own, whatever the store does, and a stalled
// store stays the store's fault. The base configuration's test holds
// max_object_size within that read-ahead.
func (o *Objects) PutObject(ctx context.Context, key string, body io.Reader, contentType string, size int64) (blobfs.Object, error) {
	r := &bodyReader{r: body}
	obj, err := o.store.Put(ctx, key, r, storage.PutOptions{ContentType: contentType, Size: size})
	if err != nil {
		switch {
		case r.err != nil && timedOut(r.err):
			return blobfs.Object{}, fmt.Errorf("%w: %w", ErrBodyTimeout, r.err)
		case r.err != nil:
			return blobfs.Object{}, fmt.Errorf("%w: %w", ErrBodyRead, r.err)
		}
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

// timedOut reports whether err is a deadline passing rather than a
// failure of its own: os.ErrDeadlineExceeded and context.DeadlineExceeded
// both report Timeout, as a net.Error.
func timedOut(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// bodyReader records the first error its reader returns other than
// io.EOF, so a failed put can tell the body's failure from the store's.
type bodyReader struct {
	r   io.Reader
	err error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && err != io.EOF && b.err == nil {
		b.err = err
	}
	return n, err
}
