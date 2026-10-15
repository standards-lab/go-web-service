package organization

import (
	"context"
	"fmt"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// The events the layer's commands emit, one per command, each committed in
// the transaction that makes the command's change true, so it is published
// exactly when the change stands, and never for a command that fails. Each
// event's subject is the id of the entity its type names: the
// organization's, or for a logo event the logo's file id. Reads, the
// seeds, and the purge that finishes a logo's delete emit nothing. Their
// types and their data are public API: a consumer in another service binds
// both.
var (
	// OrganizationCreated is the event a create emits, its data the
	// organization as the insert stored it.
	OrganizationCreated = event.Define[OrganizationData]("web-service.organization.created")
	// OrganizationEdited is the event an edit emits, its data the
	// organization as the edit left it.
	OrganizationEdited = event.Define[OrganizationData]("web-service.organization.edited")
	// OrganizationTransferred is the event a transfer emits, its data the
	// organization as the transfer left it, under its new parent.
	OrganizationTransferred = event.Define[OrganizationData]("web-service.organization.transferred")
	// OrganizationDeleted is the event a delete emits, its data the
	// organization's id and the version it was removed at.
	OrganizationDeleted = event.Define[OrganizationDeletedData]("web-service.organization.deleted")
	// LogoUploaded is the event a logo PUT emits once its logo is the
	// active one, in the transaction that activates it and releases the
	// logo it replaces; the replaced logo emits no LogoDeleted.
	LogoUploaded = event.Define[LogoUploadedData]("web-service.organization.logo.uploaded")
	// LogoDeleted is the event a logo DELETE emits, in the transaction that
	// releases the logo.
	LogoDeleted = event.Define[LogoDeletedData]("web-service.organization.logo.deleted")
)

// Recorder is the recorder the layer emits its events through, in the
// command's own transaction: go-messaging's Runtime.Recorder, which the
// composition root injects.
type Recorder = event.Recorder[*sqlate.Tx]

// raiseStored reads the organization as tx holds it, after the command
// that changed it, and raises kind about it.
func (s *store) raiseStored(ctx context.Context, tx *sqlate.Tx, q *event.Queue, kind event.EventKind[OrganizationData], id string) error {
	o, err := s.storedRows.One(ctx, tx, query.Args{"id": id})
	if err != nil {
		return fmt.Errorf("the organization %s after its change: %w", id, err)
	}
	kind.Raise(q, id, o)
	return nil
}

// raiseUploaded raises LogoUploaded for the organization's logo, the file
// its write completed. A file without the size and entity tag its
// completion records is a defect in the completion, and fails the
// activation.
func raiseUploaded(q *event.Queue, organizationID string, f blobfs.File) error {
	if f.Size == nil || f.ETag == nil {
		return fmt.Errorf("file %s completed without its size or entity tag", f.ID)
	}
	LogoUploaded.Raise(q, f.ID, LogoUploadedData{
		FileID: f.ID, OrganizationID: organizationID, ContentType: f.ContentType,
		Size: *f.Size, ETag: *f.ETag, Version: f.Version,
	})
	return nil
}
