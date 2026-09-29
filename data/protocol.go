package data

import (
	"context"
	"fmt"
	"io"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-web-sdk"
)

// Serve describes an available file as a download serves it: the object's
// description, which answers a revalidation alone, and the open that
// streams its bytes under ctx and runs only when they are sent. Any other
// file is blobfs.ErrNotFound, as an absent one is, so a pending upload or
// a file whose delete has begun is never served. It is the one file
// protocol the service keeps, since it answers in the web SDK's terms;
// the write and the delete are blobfs's (Storage.FS's Write, Ensure,
// Remove, and Purge, over Storage.Objects).
func (s *Storage) Serve(ctx context.Context, file blobfs.File) (web.Object, func() (io.ReadCloser, error), error) {
	if file.Status != blobfs.StatusAvailable || file.Size == nil || file.ETag == nil {
		return web.Object{}, nil, fmt.Errorf("file %s is %s: %w", file.ID, file.Status, blobfs.ErrNotFound)
	}
	obj := web.Object{ContentType: file.ContentType, Size: *file.Size, ETag: *file.ETag, ModifiedAt: file.UpdatedAt}
	return obj, func() (io.ReadCloser, error) { return s.Objects.Open(ctx, file.Key) }, nil
}
