package document

import (
	"context"
	"fmt"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
)

// The events the layer's commands emit, one per command, each committed in
// the transaction that makes the command's change true, so it is published
// exactly when the change stands, and never for a command that fails. Each
// event's subject is the id of the entity its type names: the directory's,
// or the file's. Reads, the sweep's reclamation of a marked branch, and the
// purges that finish a delete emit nothing. Their types and their data are
// public API: a consumer in another service binds both.
var (
	// DirectoryCreated is the event a directory's create emits, its data
	// the directory as the insert stored it. The organization's document
	// root emits it too, in the transaction that creates the root on the
	// organization's first write under the alias, a seed's among them; a
	// write that finds the root, or loses the race to create it, emits no
	// root event. The root's data shows it as the API does: no parent, and
	// named "/".
	DirectoryCreated = event.Define[DirectoryData]("web-service.document.directory.created")
	// DirectoryMoved is the event a directory's move emits, its data the
	// directory as the move left it, under its new parent.
	DirectoryMoved = event.Define[DirectoryData]("web-service.document.directory.moved")
	// DirectoryDeleted is the event a directory's delete emits: an empty
	// directory's, at the version its row was removed at, or a branch's,
	// for the marked directory alone, at the version the mark left it at.
	// A mark that finds the directory deleting already, the mark's retry,
	// emits nothing, and nothing beneath the marked directory emits.
	DirectoryDeleted = event.Define[DirectoryDeletedData]("web-service.document.directory.deleted")
	// FileUploaded is the event a document upload emits once its file is
	// available, its subject the file's id. It commits in the transaction
	// that completes the upload, so it is published exactly when the file
	// can be downloaded, and never for an upload that fails. Only the
	// upload raises it: a seed's file writes, which run blobfs's
	// retry-safe write, emit nothing.
	FileUploaded = event.Define[FileUploadedData]("web-service.document.file.uploaded")
)

// Recorder is the recorder the layer emits its events through, in the
// command's own transaction: go-messaging's Runtime.Recorder, which the
// composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// raiseDirectory raises kind about the organization's directory d, the row
// as the command that wrote it returned it, the document root presented as
// the API presents it.
func raiseDirectory(q *event.Queue, kind event.EventKind[DirectoryData], organizationID, root string, d blobfs.Directory) {
	data := DirectoryData{DirectoryID: d.ID, OrganizationID: organizationID, ParentID: d.ParentID, Name: d.Name, Version: d.Version}
	if d.ID == root {
		data.ParentID, data.Name = nil, "/"
	}
	kind.Raise(q, d.ID, data)
}

// raiseMarked raises DirectoryDeleted for the organization's directory
// with id, just marked deleting, read in tx at the version the mark left
// it at.
func (s *store) raiseMarked(ctx context.Context, tx *sqlate.Tx, q *event.Queue, organizationID, id string) error {
	d, err := s.storage.FS.Directories.Find(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("the directory %s after its mark: %w", id, err)
	}
	DirectoryDeleted.Raise(q, id, DirectoryDeletedData{DirectoryID: id, OrganizationID: organizationID, Version: d.Version})
	return nil
}

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
