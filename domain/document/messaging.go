package document

import (
	"context"
	"fmt"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
)

// FileUploaded is the event a document upload emits once its file is
// available, its subject the file's id. It commits in the transaction that
// completes the upload, so it is published exactly when the file can be
// downloaded, and never for an upload that fails. Only the upload raises
// it: a seed's file writes, which run blobfs's retry-safe write, emit
// nothing.
var FileUploaded = event.Define[FileUploadedData]("web-service.document.file.uploaded")

// Recorder is the recorder the layer emits its events through, in the
// command's own transaction: go-messaging's Runtime.Recorder, which the
// composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// emitUploaded raises FileUploaded for the organization's file, just made
// available, and writes it into tx through the recorder. A file without
// the size and entity tag its completion records is a defect in the
// completion, and fails the transaction.
func (s *store) emitUploaded(ctx context.Context, tx *sqlate.Tx, organizationID string, f blobfs.File) error {
	if f.Size == nil || f.ETag == nil {
		return fmt.Errorf("file %s completed without its size or entity tag", f.ID)
	}
	_, err := s.events.Emit(ctx, func(_ *sqlate.Tx, q *event.Queue) (struct{}, error) {
		FileUploaded.Raise(q, f.ID, FileUploadedData{
			FileID: f.ID, OrganizationID: organizationID, DirectoryID: f.DirectoryID, Name: f.Name,
			ContentType: f.ContentType, Size: *f.Size, ETag: *f.ETag, Version: f.Version,
		})
		return struct{}{}, nil
	})(tx)
	return err
}
