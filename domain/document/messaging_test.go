package document_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

func noEvent(t *testing.T, rec *sqltest.Recorder) {
	t.Helper()
	if w := outboxtest.Writes(rec); len(w) != 0 {
		t.Errorf("events written = %d; want none", len(w))
	}
}

func TestUploadFile_EmitsFileUploadedInTheCompletion(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), exec(1),
	)
	if _, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t)); err != nil {
		t.Fatalf("UploadFile = %v", err)
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, x, commit)
	writes := outboxtest.Writes(rec)
	if len(writes) != 1 {
		t.Fatalf("events written = %d; want one", len(writes))
	}
	outboxtest.Want(t, outboxtest.Decode(t, writes[0]), "web-service.document.file.uploaded", fileID, map[string]any{
		"file_id": fileID, "organization_id": orgID, "directory_id": dirID, "name": "report.txt",
		"content_type": "text/plain", "size": float64(6), "etag": `"etag-` + fileID + `"`, "version": float64(2),
	})
}

func TestUploadFile_AFailedEmitRollsBackTheCompletion(t *testing.T) {
	deleteBegunAtThePendingVersion := datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 2))
	s, rec, fake := serviceOver(t,
		root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), sqltest.Response{Err: errors.New("the outbox refused the row")},
		deleteBegunAtThePendingVersion, purged(),
	)
	_, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t))
	if err == nil || !strings.Contains(err.Error(), "the outbox refused the row") {
		t.Fatalf("UploadFile = %v; want the event's write refused", err)
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, x, sqltest.OpRollback, begin, q, commit, x)
	abandon := rec.Calls()[len(rec.Calls())-3]
	if !strings.Contains(abandon.SQL, "status = 'deleting'") || abandon.Args[1] != int64(1) {
		t.Errorf("abandon = %q %v; want the delete begun at the pending version", abandon.SQL, abandon.Args)
	}
	if _, err := fake.Get(context.Background(), fileID+"/report.txt", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the abandoned upload's object is still stored: %v", err)
	}
}

func written() sqltest.Response { return sqltest.Response{Affected: 1} }

func TestCommands_EmitTheirEventInTheirTransaction(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*document.Service) error
		ops       []sqltest.Op
		typ       string
		subject   string
		data      map[string]any
	}{
		"create": {
			[]sqltest.Response{root(rootID), within(true), datatest.DirectoryRows(directory(subID, dirID, "drafts", 1)), written()},
			func(s *document.Service) error {
				_, err := s.CreateDirectory(ctx, orgID, document.CreateDirectory{ParentID: dirID, Name: "drafts"})
				return err
			},
			[]sqltest.Op{begin, q, q, q, x, commit},
			document.DirectoryCreated.Type(), subID,
			map[string]any{"directory_id": subID, "organization_id": orgID, "parent_id": dirID, "name": "drafts", "version": float64(1)},
		},
		"create under the alias, the root found": {
			[]sqltest.Response{root(rootID), root(rootID), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), written()},
			func(s *document.Service) error {
				_, err := s.CreateDirectory(ctx, orgID, document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"})
				return err
			},
			[]sqltest.Op{q, begin, q, q, x, commit},
			document.DirectoryCreated.Type(), dirID,
			map[string]any{"directory_id": dirID, "organization_id": orgID, "parent_id": rootID, "name": "reports", "version": float64(1)},
		},
		"move": {
			[]sqltest.Response{
				root(rootID), within(true), within(true), within(false),
				datatest.DirectoryRows(directory(subID, dirID, "archive", 3)), written(),
			},
			func(s *document.Service) error {
				_, err := s.MoveDirectory(ctx, orgID, subID, 2, document.MoveDirectory{ParentID: dirID, Name: "archive"})
				return err
			},
			[]sqltest.Op{begin, q, q, q, q, q, x, commit},
			document.DirectoryMoved.Type(), subID,
			map[string]any{"directory_id": subID, "organization_id": orgID, "parent_id": dirID, "name": "archive", "version": float64(3)},
		},
		"move into the root": {
			[]sqltest.Response{
				root(rootID), within(true), within(false),
				datatest.DirectoryRows(directory(subID, rootID, "archive", 3)), written(),
			},
			func(s *document.Service) error {
				_, err := s.MoveDirectory(ctx, orgID, subID, 2, document.MoveDirectory{ParentID: document.RootAlias, Name: "archive"})
				return err
			},
			[]sqltest.Op{begin, q, q, q, q, x, commit},
			document.DirectoryMoved.Type(), subID,
			map[string]any{"directory_id": subID, "organization_id": orgID, "parent_id": rootID, "name": "archive", "version": float64(3)},
		},
		"delete": {
			[]sqltest.Response{root(rootID), within(true), exec(1), written()},
			func(s *document.Service) error { return s.DeleteDirectory(ctx, orgID, dirID, 4) },
			[]sqltest.Op{begin, q, q, x, x, commit},
			document.DirectoryDeleted.Type(), dirID,
			map[string]any{"directory_id": dirID, "organization_id": orgID, "version": float64(4)},
		},
		"delete the root": {
			[]sqltest.Response{root(rootID), exec(1), written()},
			func(s *document.Service) error { return s.DeleteDirectory(ctx, orgID, document.RootAlias, 2) },
			[]sqltest.Op{begin, q, x, x, commit},
			document.DirectoryDeleted.Type(), rootID,
			map[string]any{"directory_id": rootID, "organization_id": orgID, "version": float64(2)},
		},
		"move file": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(true),
				datatest.FileRows(file(fileID, subID, blobfs.StatusAvailable, 3)), written(),
			},
			func(s *document.Service) error {
				_, err := s.MoveFile(ctx, orgID, fileID, 2, document.MoveFile{DirectoryID: subID, Name: "report.txt"})
				return err
			},
			[]sqltest.Op{begin, q, q, q, q, q, x, commit},
			document.FileMoved.Type(), fileID,
			map[string]any{
				"file_id": fileID, "organization_id": orgID, "directory_id": subID, "name": "report.txt",
				"content_type": "text/plain", "size": float64(6), "etag": `"etag-` + fileID + `"`, "version": float64(3),
			},
		},
		"move a pending file": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)), within(true),
				datatest.FileRows(file(fileID, rootID, blobfs.StatusPending, 2)), written(),
			},
			func(s *document.Service) error {
				_, err := s.MoveFile(ctx, orgID, fileID, 1, document.MoveFile{DirectoryID: document.RootAlias, Name: "report.txt"})
				return err
			},
			[]sqltest.Op{begin, q, q, q, q, x, commit},
			document.FileMoved.Type(), fileID,
			map[string]any{
				"file_id": fileID, "organization_id": orgID, "directory_id": rootID, "name": "report.txt",
				"content_type": "text/plain", "size": nil, "etag": nil, "version": float64(2),
			},
		},
		"delete file": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true),
				datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 4)), written(),
				purged(),
			},
			func(s *document.Service) error { return s.DeleteFile(ctx, orgID, fileID, 3) },
			[]sqltest.Op{begin, q, q, q, q, x, commit, x},
			document.FileDeleted.Type(), fileID,
			map[string]any{"file_id": fileID, "organization_id": orgID, "version": float64(4)},
		},
		"delete branch": {
			[]sqltest.Response{
				root(rootID), within(true), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)),
				directoriesMarked(3), filesMarked(5),
				datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1))), written(),
			},
			func(s *document.Service) error {
				_, err := s.DeleteBranch(ctx, orgID, dirID, 1)
				return err
			},
			[]sqltest.Op{begin, q, q, q, x, x, q, x, commit},
			document.DirectoryDeleted.Type(), dirID,
			map[string]any{"directory_id": dirID, "organization_id": orgID, "version": float64(2)},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, c.responses...)
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

func TestEnsureRoot_TheTransactionThatCreatesTheRootEmitsIt(t *testing.T) {
	ctx := context.Background()
	rootRow := directory(rootID, blobfs.RootID, orgID, 1)
	created := []sqltest.Response{root(), organization(), datatest.DirectoryRows(), datatest.DirectoryRows(rootRow), exec(1), written()}
	nameTaken := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintUniqueDirectoryParentName, Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}}
	bound := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "uq_organization_directory_organization", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}}
	theWrite := []sqltest.Response{root(rootID), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), written()}
	rootData := map[string]any{"directory_id": rootID, "organization_id": orgID, "parent_id": nil, "name": "/", "version": float64(1)}
	dirData := map[string]any{"directory_id": dirID, "organization_id": orgID, "parent_id": rootID, "name": "reports", "version": float64(1)}
	cases := map[string]struct {
		responses []sqltest.Response
		ops       []sqltest.Op
		fails     bool
		events    []map[string]any
	}{
		"created": {
			append(created, theWrite...),
			[]sqltest.Op{q, begin, q, q, q, x, x, commit, begin, q, q, x, commit},
			false, []map[string]any{rootData, dirData},
		},
		"lost the race, the insert refused": {
			append([]sqltest.Response{root(), organization(), datatest.DirectoryRows(), nameTaken, root(rootID)}, theWrite...),
			[]sqltest.Op{q, begin, q, q, q, rollback, q, begin, q, q, x, commit},
			false, []map[string]any{dirData},
		},
		"lost the race, the bind refused": {
			append([]sqltest.Response{root(), organization(), datatest.DirectoryRows(rootRow), bound, root(rootID)}, theWrite...),
			[]sqltest.Op{q, begin, q, q, x, rollback, q, begin, q, q, x, commit},
			false, []map[string]any{dirData},
		},
		"created, the write refused": {
			append(created, root(rootID), nameTaken),
			[]sqltest.Op{q, begin, q, q, q, x, x, commit, begin, q, q, rollback},
			true, []map[string]any{rootData},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, c.responses...)
			_, err := s.CreateDirectory(ctx, orgID, document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"})
			if (err != nil) != c.fails {
				t.Fatalf("CreateDirectory = %v; want failure %t", err, c.fails)
			}
			sameOps(t, rec, c.ops...)
			events := outboxtest.Committed(t, rec)
			if len(events) != len(c.events) {
				t.Fatalf("events committed = %d; want %d", len(events), len(c.events))
			}
			for i, e := range events {
				outboxtest.Want(t, e, document.DirectoryCreated.Type(), c.events[i]["directory_id"].(string), c.events[i])
			}
		})
	}
}

func TestDeleteBranch_ARetryOfTheMarkEmitsNothing(t *testing.T) {
	marked := datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1)))
	s, rec, _, _ := serviceOnPostgres(t,
		root(rootID), within(true), treeLocked(), marked,
		treeLocked(), directoriesMarked(0), marked,
		exec(0),
	)
	if id, err := s.DeleteBranch(context.Background(), orgID, dirID, 1); err != nil || id != dirID {
		t.Fatalf("DeleteBranch = %q, %v; want the retry accepted", id, err)
	}
	sameOps(t, rec, begin, q, q, x, q, x, x, q, x, commit)
	noEvent(t, rec)
}

func TestDeleteBranch_AMarkOvertakenOnTheBaselineEmitsNothing(t *testing.T) {
	readActive := datatest.DirectoryRows(directory(dirID, rootID, "reports", 1))
	markedMeanwhile := datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 2)))
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), readActive,
		directoriesMarked(0), markedMeanwhile,
		exec(0),
	)
	if id, err := s.DeleteBranch(context.Background(), orgID, dirID, 1); err != nil || id != dirID {
		t.Fatalf("DeleteBranch = %q, %v; want the retry accepted", id, err)
	}
	sameOps(t, rec, begin, q, q, q, x, q, x, commit)
	noEvent(t, rec)
}

func TestDeleteBranch_ARetryThatMarksAStragglerEmitsNothing(t *testing.T) {
	s, rec, _, _ := serviceOnPostgres(t,
		root(rootID), within(true), treeLocked(), datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1))),
		treeLocked(),
		directoriesMarked(1), filesMarked(2),
	)
	if id, err := s.DeleteBranch(context.Background(), orgID, dirID, 1); err != nil || id != dirID {
		t.Fatalf("DeleteBranch = %q, %v; want the retry accepted", id, err)
	}
	sameOps(t, rec, begin, q, q, x, q, x, x, x, commit)
	noEvent(t, rec)
}

func TestDeleteFile_ARetryEmitsNothingAndPurges(t *testing.T) {
	deletingRow := datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 4))
	s, rec, fake := serviceOver(t,
		root(rootID), deletingRow, within(true),
		guardedUpdateMatchesNothing(), deletingRow,
		purged(),
	)
	ctx := context.Background()
	key := fileID + "/report.txt"
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile(ctx, orgID, fileID, 1); err != nil {
		t.Fatalf("DeleteFile = %v; want the retry accepted", err)
	}
	sameOps(t, rec, begin, q, q, q, q, q, commit, x)
	noEvent(t, rec)
	if _, err := fake.Get(ctx, key, storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the retried delete's object is still stored: %v", err)
	}
}

func TestCommands_AFailedCommandCommitsNoEvent(t *testing.T) {
	ctx := context.Background()
	stale := datatest.DirectoryRows(directory(dirID, rootID, "reports", 2))
	taken := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintUniqueDirectoryParentName, Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}}
	refused := sqltest.Response{Err: errors.New("the outbox refused the row")}
	create := func(parent string) func(*document.Service) error {
		return func(s *document.Service) error {
			_, err := s.CreateDirectory(ctx, orgID, document.CreateDirectory{ParentID: parent, Name: "drafts"})
			return err
		}
	}
	move := func(id string) func(*document.Service) error {
		return func(s *document.Service) error {
			_, err := s.MoveDirectory(ctx, orgID, id, 1, document.MoveDirectory{ParentID: dirID, Name: "archive"})
			return err
		}
	}
	branch := func(s *document.Service) error {
		_, err := s.DeleteBranch(ctx, orgID, dirID, 1)
		return err
	}
	moveFile := func(dir string) func(*document.Service) error {
		return func(s *document.Service) error {
			_, err := s.MoveFile(ctx, orgID, fileID, 2, document.MoveFile{DirectoryID: dir, Name: "report.txt"})
			return err
		}
	}
	deleteFile := func(s *document.Service) error { return s.DeleteFile(ctx, orgID, fileID, 2) }
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*document.Service) error
		ops       []sqltest.Op
		want      error
	}{
		"create, a parent outside the root": {
			[]sqltest.Response{root(rootID), within(false)}, create(otherID),
			[]sqltest.Op{begin, q, q, rollback}, blobfs.ErrNotFound,
		},
		"create, a taken name": {
			[]sqltest.Response{root(rootID), within(true), taken}, create(dirID),
			[]sqltest.Op{begin, q, q, q, rollback}, blobfs.ErrNameTaken,
		},
		"create, the event's write refused": {
			[]sqltest.Response{root(rootID), within(true), datatest.DirectoryRows(directory(subID, dirID, "drafts", 1)), refused}, create(dirID),
			[]sqltest.Op{begin, q, q, q, x, rollback}, nil,
		},
		"create, a missing organization": {
			[]sqltest.Response{root(), {Columns: []string{"id"}}}, create(document.RootAlias),
			[]sqltest.Op{q, begin, q, rollback}, nil,
		},
		"move, a cycle": {
			[]sqltest.Response{root(rootID), within(true), within(true), within(true)}, move(subID),
			[]sqltest.Op{begin, q, q, q, q, rollback}, blobfs.ErrCycle,
		},
		"move, the root by its id": {
			[]sqltest.Response{root(rootID)}, move(rootID),
			[]sqltest.Op{begin, q, rollback}, document.ErrValidation,
		},
		"move, a taken name": {
			[]sqltest.Response{root(rootID), within(true), within(true), within(false), taken}, move(subID),
			[]sqltest.Op{begin, q, q, q, q, q, rollback}, blobfs.ErrNameTaken,
		},
		"delete, a stale version": {
			[]sqltest.Response{root(rootID), within(true), exec(0), stale},
			func(s *document.Service) error { return s.DeleteDirectory(ctx, orgID, dirID, 1) },
			[]sqltest.Op{begin, q, q, x, q, rollback}, query.ErrVersionMismatch,
		},
		"delete, not empty": {
			[]sqltest.Response{root(rootID), within(true), {Err: notEmpty()}},
			func(s *document.Service) error { return s.DeleteDirectory(ctx, orgID, dirID, 1) },
			[]sqltest.Op{begin, q, q, x, rollback}, blobfs.ErrNotEmpty,
		},
		"delete, the event's write refused": {
			[]sqltest.Response{root(rootID), within(true), exec(1), refused},
			func(s *document.Service) error { return s.DeleteDirectory(ctx, orgID, dirID, 1) },
			[]sqltest.Op{begin, q, q, x, x, rollback}, nil,
		},
		"move file, a stale version": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true), within(true),
				guardedUpdateMatchesNothing(), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)),
				datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), datatest.DirectoryRows(directory(subID, dirID, "drafts", 1)),
			}, moveFile(subID),
			[]sqltest.Op{begin, q, q, q, q, q, q, q, q, rollback}, query.ErrVersionMismatch,
		},
		"move file, outside the root": {
			[]sqltest.Response{root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(false)}, moveFile(otherID),
			[]sqltest.Op{begin, q, q, q, q, rollback}, blobfs.ErrNotFound,
		},
		"move file, a taken name": {
			[]sqltest.Response{root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(true), {Err: takenName()}}, moveFile(subID),
			[]sqltest.Op{begin, q, q, q, q, q, rollback}, blobfs.ErrNameTaken,
		},
		"move file, the file deleting": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(true),
				datatest.FileRows(), datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 3)), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)),
			}, moveFile(subID),
			[]sqltest.Op{begin, q, q, q, q, q, q, q, rollback}, blobfs.ErrDeleting,
		},
		"move file, the event's write refused": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(true),
				datatest.FileRows(file(fileID, subID, blobfs.StatusAvailable, 3)), refused,
			}, moveFile(subID),
			[]sqltest.Op{begin, q, q, q, q, q, x, rollback}, nil,
		},
		"delete file, a stale version": {
			[]sqltest.Response{root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true), datatest.FileRows(), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3))},
			deleteFile, []sqltest.Op{begin, q, q, q, q, q, rollback}, query.ErrVersionMismatch,
		},
		"delete file, outside the root": {
			[]sqltest.Response{root(rootID), datatest.FileRows(file(fileID, otherID, blobfs.StatusAvailable, 2)), within(false)},
			deleteFile, []sqltest.Op{begin, q, q, q, rollback}, blobfs.ErrNotFound,
		},
		"delete file, gone": {
			[]sqltest.Response{root(rootID), datatest.FileRows()},
			deleteFile, []sqltest.Op{begin, q, q, rollback}, blobfs.ErrNotFound,
		},
		"delete file, the event's write refused": {
			[]sqltest.Response{
				root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true),
				datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 3)), refused,
			},
			deleteFile, []sqltest.Op{begin, q, q, q, q, x, rollback}, nil,
		},
		"delete branch, a stale version": {
			[]sqltest.Response{root(rootID), within(true), stale, exec(0), stale}, branch,
			[]sqltest.Op{begin, q, q, q, x, q, rollback}, query.ErrVersionMismatch,
		},
		"delete branch, outside the root": {
			[]sqltest.Response{root(rootID), within(false)}, branch,
			[]sqltest.Op{begin, q, q, rollback}, blobfs.ErrNotFound,
		},
		"delete branch, the event's write refused": {
			[]sqltest.Response{root(rootID), within(true), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), exec(1), exec(0), datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1))), refused}, branch,
			[]sqltest.Op{begin, q, q, q, x, x, q, x, rollback}, nil,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, c.responses...)
			err := c.run(s)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("= %v; want the command's failure %v", err, c.want)
			}
			sameOps(t, rec, c.ops...)
			if events := outboxtest.Committed(t, rec); len(events) != 0 {
				t.Errorf("events committed = %d; want none", len(events))
			}
		})
	}
}
