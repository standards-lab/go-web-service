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
// or the file's. A seed that creates a root, a directory, or a file emits
// the event its creation's command emits, with the same data, in the
// transaction that creates it. Reads, the sweep's reclamation of a marked
// branch, and the purges that finish a delete emit nothing. Their types
// and their data are public API: a consumer in another service binds both.
var (
	// DirectoryCreated is the event a directory's create emits, its data
	// the directory as the insert stored it. The organization's document
	// root emits it too, in the transaction that creates the root on the
	// organization's first write under the alias, a seed's among them; a
	// write that finds the root, or loses the race to create it, emits no
	// root event. The root's data shows it as the API does: no parent, and
	// named "/". A seed's directory emits it in the transaction that
	// creates it, and one the seed finds emits nothing.
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
	// can be downloaded, and never for an upload that fails. A seed's file
	// emits it too, in the completion's transaction of blobfs's retry-safe
	// write when that write completes the file; a file the seed finds
	// complete emits nothing.
	FileUploaded = event.Define[FileUploadedData]("web-service.document.file.uploaded")
	// FileMoved is the event a file's move emits, its data the file as the
	// move left it, under its new directory and name.
	FileMoved = event.Define[FileUploadedData]("web-service.document.file.moved")
	// FileDeleted is the event a file's delete emits, in the transaction
	// that begins the delete, at the version that left the file deleting.
	// A delete that finds the file deleting already, the delete's retry,
	// emits nothing, and neither does the purge that finishes it.
	FileDeleted = event.Define[FileDeletedData]("web-service.document.file.deleted")
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
	return s.emit(ctx, tx, func(q *event.Queue) { FileUploaded.Raise(q, f.ID, fileData(organizationID, f)) })
}

// emitDeleted raises FileDeleted for the organization's file, just moved
// to deleting, at the version its delete left it at, and writes it into
// tx through the recorder.
func (s *store) emitDeleted(ctx context.Context, tx *sqlate.Tx, organizationID string, f blobfs.File) error {
	return s.emit(ctx, tx, func(q *event.Queue) {
		FileDeleted.Raise(q, f.ID, FileDeletedData{FileID: f.ID, OrganizationID: organizationID, Version: f.Version})
	})
}

// emit writes the events raise raises into tx through the recorder, for a
// hook a protocol runs in its own transaction.
func (s *store) emit(ctx context.Context, tx *sqlate.Tx, raise func(*event.Queue)) error {
	_, err := s.events.Emit(ctx, func(_ *sqlate.Tx, q *event.Queue) (struct{}, error) {
		raise(q)
		return struct{}{}, nil
	})(tx)
	return err
}

// fileData is the organization's file f as FileUploaded and FileMoved
// carry it: its stored state, as the command that wrote it returned it.
func fileData(organizationID string, f blobfs.File) FileUploadedData {
	return FileUploadedData{
		FileID: f.ID, OrganizationID: organizationID, DirectoryID: f.DirectoryID, Name: f.Name,
		ContentType: f.ContentType, Size: f.Size, ETag: f.ETag, Version: f.Version,
	}
}
