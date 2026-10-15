package organization_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

// Each command writes exactly one event, its kind's, in the transaction
// that makes its change true: the outbox insert is the last statement
// before that transaction's commit. Its subject is the id of the entity
// the type names, and its data the entity's stored state after the
// command, under the API's field names, every field checked: no path, no
// timestamps, no earlier state. A replacement logo emits LogoUploaded
// alone, no LogoDeleted for the logo it replaces, and the purges after the
// commits emit nothing.
func TestCommands_EmitTheirEventInTheirTransaction(t *testing.T) {
	ctx := context.Background()
	etag := `"etag-` + newFileID + `"`
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*organization.Service) error
		ops       []sqltest.Op
		typ       string
		subject   string
		data      map[string]any
	}{
		"create": {
			[]sqltest.Response{identity(validID, 1), stored(validID, "eng", "Eng", 1, parentID), written()},
			func(s *organization.Service) error {
				p := parentID
				_, err := s.Create(ctx, organization.CreateOrganization{ParentID: &p, Code: "eng", Name: "Eng"})
				return err
			},
			[]sqltest.Op{begin, q, q, x, commit},
			organization.OrganizationCreated.Type(), validID,
			map[string]any{"organization_id": validID, "parent_id": parentID, "code": "eng", "name": "Eng", "version": float64(1)},
		},
		"edit": {
			[]sqltest.Response{exec(1), stored(validID, "eng", "Engineering", 4, parentID), written()},
			func(s *organization.Service) error {
				_, err := s.Edit(ctx, validID, 3, organization.EditOrganization{Code: "eng", Name: "Engineering"})
				return err
			},
			[]sqltest.Op{begin, x, q, x, commit},
			organization.OrganizationEdited.Type(), validID,
			map[string]any{"organization_id": validID, "parent_id": parentID, "code": "eng", "name": "Engineering", "version": float64(4)},
		},
		"transfer": {
			[]sqltest.Response{exec(0), count(0), exec(1), stored(validID, "eng", "Eng", 3, parentID), written()},
			func(s *organization.Service) error {
				_, err := s.Transfer(ctx, validID, 2, transferTo(t, `{"parent_id":"`+parentID+`"}`))
				return err
			},
			[]sqltest.Op{begin, x, q, x, q, x, commit},
			organization.OrganizationTransferred.Type(), validID,
			map[string]any{"organization_id": validID, "parent_id": parentID, "code": "eng", "name": "Eng", "version": float64(3)},
		},
		"transfer to the root": {
			[]sqltest.Response{exec(0), exec(1), stored(validID, "eng", "Eng", 3), written()},
			func(s *organization.Service) error {
				_, err := s.Transfer(ctx, validID, 2, transferTo(t, `{"parent_id":null}`))
				return err
			},
			[]sqltest.Op{begin, x, x, q, x, commit},
			organization.OrganizationTransferred.Type(), validID,
			map[string]any{"organization_id": validID, "parent_id": nil, "code": "eng", "name": "Eng", "version": float64(3)},
		},
		"delete": {
			[]sqltest.Response{exec(1), written()},
			func(s *organization.Service) error { return s.Delete(ctx, validID, 5) },
			[]sqltest.Op{begin, x, x, commit},
			organization.OrganizationDeleted.Type(), validID,
			map[string]any{"organization_id": validID, "version": float64(5)},
		},
		"put logo, replacing": {
			[]sqltest.Response{
				datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
				datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
				exec(1), datatest.FileRows(file(oldFileID, blobfs.StatusAvailable, 2)), exec(1),
				datatest.FileRows(file(oldFileID, blobfs.StatusDeleting, 3)), exec(1), written(),
				exec(1), // the replaced file purged
			},
			func(s *organization.Service) error {
				_, err := s.PutLogo(ctx, validID, logoUpload(t))
				return err
			},
			[]sqltest.Op{q, begin, q, q, commit, begin, q, commit, begin, x, q, x, q, x, x, commit, x},
			organization.LogoUploaded.Type(), newFileID,
			map[string]any{"file_id": newFileID, "organization_id": validID, "content_type": "image/png", "size": float64(3), "etag": etag, "version": float64(2)},
		},
		"delete logo": {
			[]sqltest.Response{
				datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)), exec(1),
				datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), written(),
				exec(1), // the purge
			},
			func(s *organization.Service) error { return s.DeleteLogo(ctx, validID) },
			[]sqltest.Op{begin, q, x, q, x, commit, x},
			organization.LogoDeleted.Type(), newFileID,
			map[string]any{"file_id": newFileID, "organization_id": validID, "version": float64(3)},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, sqltest.ReturningDialect{}, c.responses...)
			if err := c.run(s); err != nil {
				t.Fatalf("= %v", err)
			}
			sameOps(t, rec, c.ops...)
			writes := outboxtest.Writes(rec)
			if len(writes) != 1 {
				t.Fatalf("events written = %d; want one", len(writes))
			}
			calls := rec.Calls()
			for i, call := range calls {
				if call.SQL == writes[0].SQL && i+1 < len(calls) && calls[i+1].Op != commit {
					t.Errorf("the event's write is followed by %v; want the change's commit", calls[i+1].Op)
				}
			}
			outboxtest.Want(t, outboxtest.Decode(t, writes[0]), c.typ, c.subject, c.data)
		})
	}
}

// A command that fails commits no event, however it fails: its
// transaction rolls back with the change and the event together, and the
// API's result is the failure it was. That holds for the guard's
// refusals, a cycle, a constraint violation, a missing logo, a lost
// activation, and an event whose own write fails.
func TestCommands_AFailedCommandCommitsNoEvent(t *testing.T) {
	ctx := context.Background()
	stale := []sqltest.Response{exec(0), {Columns: []string{"version"}, Rows: [][]driver.Value{{int64(9)}}}}
	unique := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "uq_organization_parent_code", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}}
	refused := sqltest.Response{Err: errors.New("the outbox refused the row")}
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*organization.Service) error
		ops       []sqltest.Op
	}{
		"create, a duplicate sibling code": {
			[]sqltest.Response{unique},
			func(s *organization.Service) error {
				_, err := s.Create(ctx, organization.CreateOrganization{Code: "eng", Name: "Eng"})
				return err
			},
			[]sqltest.Op{begin, q, rollback},
		},
		"edit, a stale version": {
			stale,
			func(s *organization.Service) error {
				_, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "eng", Name: "Eng"})
				return err
			},
			[]sqltest.Op{begin, x, q, rollback},
		},
		"edit, the event's write refused": {
			[]sqltest.Response{exec(1), stored(validID, "eng", "Eng", 2), refused},
			func(s *organization.Service) error {
				_, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "eng", Name: "Eng"})
				return err
			},
			[]sqltest.Op{begin, x, q, x, rollback},
		},
		"transfer, a cycle": {
			[]sqltest.Response{exec(0), count(1)},
			func(s *organization.Service) error {
				_, err := s.Transfer(ctx, validID, 1, transferTo(t, `{"parent_id":"`+parentID+`"}`))
				return err
			},
			[]sqltest.Op{begin, x, q, rollback},
		},
		"transfer, a stale version": {
			append([]sqltest.Response{exec(0), count(0)}, stale...),
			func(s *organization.Service) error {
				_, err := s.Transfer(ctx, validID, 1, transferTo(t, `{"parent_id":"`+parentID+`"}`))
				return err
			},
			[]sqltest.Op{begin, x, q, x, q, rollback},
		},
		"delete, a stale version": {
			stale,
			func(s *organization.Service) error { return s.Delete(ctx, validID, 1) },
			[]sqltest.Op{begin, x, q, rollback},
		},
		"delete, a child blocking it": {
			[]sqltest.Response{{Err: &sqlate.ConstraintError{Constraint: "organization_parent_id_fkey", Class: sqlate.ErrForeignKeyViolation, Err: errors.New("fk")}}},
			func(s *organization.Service) error { return s.Delete(ctx, validID, 1) },
			[]sqltest.Op{begin, x, rollback},
		},
		"put logo, a lost activation": {
			[]sqltest.Response{
				datatest.DirectoryRows(imagesDirectory()), exists(), datatest.FileRows(file(newFileID, blobfs.StatusPending, 1)),
				datatest.FileRows(file(newFileID, blobfs.StatusAvailable, 2)),
				exec(1), datatest.FileRows(),
				{Err: &sqlate.ConstraintError{Constraint: "ux_organization_image_active", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}},
				datatest.FileRows(file(newFileID, blobfs.StatusDeleting, 3)), exec(1), // the new file retired
			},
			func(s *organization.Service) error {
				_, err := s.PutLogo(ctx, validID, logoUpload(t))
				return err
			},
			[]sqltest.Op{q, begin, q, q, commit, begin, q, commit, begin, x, q, x, rollback, begin, q, commit, x},
		},
		"delete logo, none active": {
			[]sqltest.Response{datatest.FileRows()},
			func(s *organization.Service) error { return s.DeleteLogo(ctx, validID) },
			[]sqltest.Op{begin, q, rollback},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, sqltest.ReturningDialect{}, c.responses...)
			if err := c.run(s); err == nil {
				t.Fatal("= nil; want the command's failure")
			}
			sameOps(t, rec, c.ops...)
			outboxtest.NoneCommitted(t, rec)
		})
	}
}

const rollback = sqltest.OpRollback
