package document_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
)

// textUpload is a six-byte text upload as the handler reads it.
func textUpload(t *testing.T) web.Upload {
	t.Helper()
	r := httptest.NewRequest("PUT", "/", strings.NewReader("report"))
	r.Header.Set("Content-Type", "text/plain")
	u, err := web.ReadUpload(httptest.NewRecorder(), r, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// sameOps fails the test unless the recorder's operations are want.
func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", got, want)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
}

const (
	q, x          = sqltest.OpQuery, sqltest.OpExec
	begin, commit = sqltest.OpBegin, sqltest.OpCommit
)

// The file protocols end to end over the scripted driver and the fake
// object store: the upload into a directory of the root, the metadata and
// content reads, the move into the root by its alias, and the delete, each
// step on the session its protocol gives it, the scope checked first.
func TestStore_FileProtocols(t *testing.T) {
	ctx := context.Background()
	s, rec, fake := serviceOver(t,
		// PutFile: the pending row with its scope in one transaction, the
		// put, the completion on the pool.
		root(rootID), within(true), fileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)),
		// Content: the owner row, the file, its directory's scope.
		root(rootID), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true),
		// MoveFile to the root by its alias.
		root(rootID), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true),
		fileRows(file(fileID, rootID, blobfs.StatusAvailable, 3)),
		// DeleteFile: the scope and the delete begun in one transaction, the
		// object deleted, the purge on the pool.
		root(rootID), fileRows(file(fileID, rootID, blobfs.StatusAvailable, 3)),
		fileRows(file(fileID, rootID, blobfs.StatusDeleting, 4)),
		exec(1),
	)
	id, err := s.PutFile(ctx, orgID, dirID, "report.txt", textUpload(t))
	if err != nil || id != (document.Identity{ID: fileID, Version: 2}) {
		t.Fatalf("PutFile = %+v, %v", id, err)
	}
	if fake.Puts() != 1 {
		t.Errorf("puts = %d, want the upload stored once", fake.Puts())
	}
	create := rec.Calls()[3]
	if !strings.HasPrefix(create.SQL, "INSERT INTO blobfs_file") || create.Args[1] != "report.txt" || create.Args[4] != dirID {
		t.Errorf("create = %q %v; want the file named in the scoped directory", create.SQL, create.Args)
	}

	c, err := s.Content(ctx, orgID, fileID)
	if err != nil || c.Name != "report.txt" || c.Object.Size != 6 || c.Object.ETag != `"etag-`+fileID+`"` {
		t.Fatalf("Content = %+v, %v", c, err)
	}
	body, err := c.Open()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(body); string(b) != "report" {
		t.Errorf("content = %q", b)
	}
	_ = body.Close()

	moved, err := s.MoveFile(ctx, orgID, fileID, 2, document.MoveFile{DirectoryID: document.RootAlias, Name: "report.txt"})
	if err != nil || moved.Version != 3 {
		t.Fatalf("MoveFile = %+v, %v", moved, err)
	}
	if err := s.DeleteFile(ctx, orgID, fileID); err != nil {
		t.Fatalf("DeleteFile = %v", err)
	}
	if _, err := c.Open(); err == nil {
		t.Error("the file's object outlived its delete")
	}
	sameOps(t, rec,
		begin, q, q, q, commit, q,
		q, q, q,
		begin, q, q, q, q, commit,
		begin, q, q, q, commit, x,
	)
}

// An upload into the alias of an organization without a root ensures it
// first, the directory and its owner row in one transaction.
func TestStore_PutFileEnsuresTheRoot(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(), organization(), dirRows(), dirRows(directory(rootID, blobfs.RootID, orgID, 1)), exec(1),
		root(rootID), fileRows(file(fileID, rootID, blobfs.StatusPending, 1)),
		fileRows(file(fileID, rootID, blobfs.StatusAvailable, 2)),
	)
	if _, err := s.PutFile(context.Background(), orgID, document.RootAlias, "report.txt", textUpload(t)); err != nil {
		t.Fatalf("PutFile = %v", err)
	}
	sameOps(t, rec,
		q, begin, q, q, q, x, commit,
		begin, q, q, commit, q,
	)
}

// A first write for an organization that does not exist is the missing
// row, before any directory is written.
func TestStore_EnsureRootForAMissingOrganizationIsNoRows(t *testing.T) {
	s, rec, _ := serviceOver(t, root(), sqltest.Response{Columns: []string{"id"}})
	_, err := s.CreateDirectory(context.Background(), orgID, document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"})
	if err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("CreateDirectory = %v; want the missing organization", err)
	}
	sameOps(t, rec, q, begin, q, sqltest.OpRollback)
}

// A put that fails retires the pending row, so the name is free for a
// retry.
func TestStore_AFailedPutRetiresThePendingRow(t *testing.T) {
	s, rec, fake := serviceOver(t,
		root(rootID), within(true), fileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		fileRows(file(fileID, dirID, blobfs.StatusDeleting, 2)), exec(1),
	)
	fake.FailPut(errors.New("put failed"))
	if _, err := s.PutFile(context.Background(), orgID, dirID, "report.txt", textUpload(t)); err == nil {
		t.Fatal("PutFile succeeded over a failed put")
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, commit, x)
}

// The directory reads: the metadata with its path relative to the root
// and its status, and the listings, lowered onto blobfs's with the
// request's sort and filters and blobfs's own filter hiding deleting rows,
// each followed by blobfs's read of the listed directory.
func TestStore_DirectoryReads(t *testing.T) {
	ctx := context.Background()
	ancestors := sqltest.Response{
		Columns: []string{"id", "parent_id", "name"},
		Rows: [][]driver.Value{
			{dirID, rootID, "reports"},
			{rootID, blobfs.RootID, orgID},
			{blobfs.RootID, nil, "/"},
		},
	}
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), dirRows(directory(dirID, rootID, "reports", 1)), ancestors,
		root(rootID), dirRows(directory(rootID, blobfs.RootID, orgID, 1)),
		sqltest.Response{Columns: []string{"id", "parent_id", "name"}, Rows: [][]driver.Value{{rootID, blobfs.RootID, orgID}, {blobfs.RootID, nil, "/"}}},
		root(rootID), sqltest.WithTotal(dirRows(directory(dirID, rootID, "reports", 1)), 1),
		dirRows(directory(rootID, blobfs.RootID, orgID, 1)),
		root(rootID), within(true), sqltest.WithTotal(fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), 1),
		dirRows(directory(dirID, rootID, "reports", 1)),
	)
	d, err := s.Directory(ctx, orgID, dirID)
	if err != nil || d.Path != "/reports" || d.Name != "reports" || *d.ParentID != rootID || d.Status != blobfs.DirectoryStatusActive {
		t.Fatalf("Directory = %+v, %v", d, err)
	}
	r, err := s.Directory(ctx, orgID, document.RootAlias)
	if err != nil || r.Path != "/" || r.Name != "/" || r.ParentID != nil || r.ID != rootID {
		t.Fatalf("Directory(root) = %+v, %v", r, err)
	}
	limits := web.Limits{DefaultSize: 20, MaxSize: 100, Cursor: true}
	query, _ := web.ParseQuery(url.Values{"sort": {"-created_at"}}, limits)
	dirs, paging, err := s.ListDirectories(ctx, orgID, document.RootAlias, query)
	if err != nil || len(dirs) != 1 || dirs[0].ID != dirID || paging.Total != 1 || paging.More {
		t.Fatalf("ListDirectories = %+v, %+v, %v", dirs, paging, err)
	}
	query, _ = web.ParseQuery(url.Values{"status": {"available"}}, limits)
	files, paging, err := s.ListFiles(ctx, orgID, dirID, query)
	if err != nil || len(files) != 1 || files[0].Status != blobfs.StatusAvailable || paging.Total != 1 {
		t.Fatalf("ListFiles = %+v, %+v, %v", files, paging, err)
	}
	sqls := rec.SQL(q)
	if list := sqls[8]; !strings.Contains(list, "ORDER BY q.created_at DESC, q.name DESC") || !strings.Contains(list, "q.status <> CAST(") {
		t.Errorf("directory listing = %q; want the request's sort with the name tie-breaker, deleting rows hidden", list)
	}
	if list := sqls[len(sqls)-2]; !strings.Contains(list, "q.status = CAST(") || !strings.Contains(list, "q.status <> CAST(") {
		t.Errorf("file listing = %q; want the request's filter, deleting rows hidden", list)
	}
}

// A directory in a branch marked for its delete reads by id with its
// status, while its listings are not found: blobfs refuses them as
// deleting, and a listing answers that as the missing directory, not a
// conflict.
func TestStore_ADeletingDirectory(t *testing.T) {
	ctx := context.Background()
	marked := deleting(directory(dirID, rootID, "reports", 1))
	s, _, _ := serviceOver(t,
		root(rootID), within(true), dirRows(marked),
		sqltest.Response{
			Columns: []string{"id", "parent_id", "name"},
			Rows:    [][]driver.Value{{dirID, rootID, "reports"}, {rootID, blobfs.RootID, orgID}, {blobfs.RootID, nil, "/"}},
		},
		root(rootID), within(true), sqltest.WithTotal(dirRows(), 0), dirRows(marked),
		root(rootID), within(true), sqltest.WithTotal(fileRows(), 0), dirRows(marked),
	)
	d, err := s.Directory(ctx, orgID, dirID)
	if err != nil || d.Status != blobfs.DirectoryStatusDeleting || d.Version != 2 {
		t.Fatalf("Directory = %+v, %v; want it deleting", d, err)
	}
	q, _ := web.ParseQuery(url.Values{}, web.Limits{DefaultSize: 20, MaxSize: 100})
	if _, _, err := s.ListDirectories(ctx, orgID, dirID, q); !errors.Is(err, blobfs.ErrNotFound) || errors.Is(err, blobfs.ErrDeleting) {
		t.Errorf("ListDirectories = %v; want not found, and not deleting", err)
	}
	if _, _, err := s.ListFiles(ctx, orgID, dirID, q); !errors.Is(err, blobfs.ErrNotFound) || errors.Is(err, blobfs.ErrDeleting) {
		t.Errorf("ListFiles = %v; want not found, and not deleting", err)
	}
}

// An organization without a root lists nothing under the alias, and a
// specific id is not found.
func TestStore_ReadsBeforeTheRoot(t *testing.T) {
	ctx := context.Background()
	s, _, _ := serviceOver(t, root(), root())
	q, _ := web.ParseQuery(url.Values{}, web.Limits{DefaultSize: 20, MaxSize: 100})
	items, paging, err := s.ListFiles(ctx, orgID, document.RootAlias, q)
	if err != nil || len(items) != 0 || paging.Total != 0 {
		t.Fatalf("ListFiles(root) = %v, %+v, %v; want an empty page", items, paging, err)
	}
	if _, _, err := s.ListFiles(ctx, orgID, dirID, q); err == nil {
		t.Fatal("ListFiles(id) before the root found a directory")
	}
}

// A directory move: the scope of the directory and of its destination in
// the move's transaction, then blobfs's cycle check and guarded update.
func TestStore_MoveDirectory(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), within(true), // the directory and its destination in scope
		within(false), // blobfs's cycle check
		dirRows(directory(subID, dirID, "archive", 2)), // the guarded update
	)
	id, err := s.MoveDirectory(context.Background(), orgID, subID, 1, document.MoveDirectory{ParentID: dirID, Name: "archive"})
	if err != nil || id != (document.Identity{ID: subID, Version: 2}) {
		t.Fatalf("MoveDirectory = %+v, %v", id, err)
	}
	sameOps(t, rec, begin, q, q, q, q, q, commit)
}

// The root is not moved, by its alias before any I/O or by its id once
// the owner row names it.
func TestStore_MoveDirectoryRefusesTheRoot(t *testing.T) {
	s, rec, _ := serviceOver(t, root(rootID))
	m := document.MoveDirectory{ParentID: dirID, Name: "x"}
	if _, err := s.MoveDirectory(context.Background(), orgID, document.RootAlias, 1, m); !errors.Is(err, document.ErrValidation) {
		t.Errorf("move of the alias = %v; want a validation rejection", err)
	}
	if _, err := s.MoveDirectory(context.Background(), orgID, rootID, 1, m); !errors.Is(err, document.ErrValidation) {
		t.Errorf("move of the root's id = %v; want a validation rejection", err)
	}
	sameOps(t, rec, begin, q, sqltest.OpRollback)
}

// A destination outside the organization's root is not found, and nothing
// moves.
func TestStore_MoveOutsideTheRootIsNotFound(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(rootID), within(true), within(false), // the directory in scope, the destination not
		root(rootID), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(false),
	)
	ctx := context.Background()
	if _, err := s.MoveDirectory(ctx, orgID, subID, 1, document.MoveDirectory{ParentID: otherID, Name: "x"}); !errors.Is(err, blobfs.ErrNotFound) {
		t.Errorf("MoveDirectory = %v; want not found", err)
	}
	if _, err := s.MoveFile(ctx, orgID, fileID, 2, document.MoveFile{DirectoryID: otherID, Name: "x"}); !errors.Is(err, blobfs.ErrNotFound) {
		t.Errorf("MoveFile = %v; want not found", err)
	}
	sameOps(t, rec, begin, q, q, q, sqltest.OpRollback, begin, q, q, q, q, sqltest.OpRollback)
}

// The recursive delete of the root: the bounded walk removes each file by
// the delete protocol and each directory after its own contents, deepest
// first, then the root with its owner row.
func TestStore_RecursiveDeleteWalksChildrenFirst(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(rootID),
		// The root's first pass: one file, then one directory.
		fileRows(file(fileID, rootID, blobfs.StatusAvailable, 2)),
		fileRows(file(fileID, rootID, blobfs.StatusDeleting, 3)), exec(1),
		dirRows(directory(subID, rootID, "archive", 1)),
		// The directory's first pass: one file, no directory.
		fileRows(file(file2ID, subID, blobfs.StatusAvailable, 2)),
		fileRows(file(file2ID, subID, blobfs.StatusDeleting, 3)), exec(1),
		dirRows(),
		// The directory's second pass finds it empty, and it is removed.
		fileRows(), dirRows(), exec(1),
		// The root's second pass finds it empty; the root and its owner row go.
		fileRows(), dirRows(),
		exec(1), exec(1),
	)
	if err := s.DeleteDirectory(context.Background(), orgID, document.RootAlias, true); err != nil {
		t.Fatalf("DeleteDirectory = %v", err)
	}
	sameOps(t, rec,
		q,
		q, begin, q, commit, x,
		q,
		q, begin, q, commit, x,
		q,
		q, q, x,
		q, q,
		begin, x, x, commit,
	)
	var removed []string
	for _, c := range rec.Calls() {
		if strings.HasPrefix(c.SQL, "DELETE FROM blobfs_directory") {
			removed = append(removed, fmt.Sprint(c.Args[0]))
		}
		// The walk lists every status, so a file a stopped delete left
		// deleting is finished rather than holding its directory.
		if strings.Contains(c.SQL, "LIMIT") && strings.Contains(c.SQL, "q.status") {
			t.Errorf("walk listing = %q; want every status listed", c.SQL)
		}
	}
	if fmt.Sprint(removed) != fmt.Sprint([]string{subID, rootID}) {
		t.Errorf("removed %v; want the child before the root", removed)
	}
}

// Without recursive, a directory with contents is not empty: the foreign
// key's refusal, classified by blobfs.
func TestStore_DeleteOfANonEmptyDirectoryIsNotEmpty(t *testing.T) {
	s, _, _ := serviceOver(t, root(rootID), within(true), sqltest.Response{Err: notEmpty()})
	if err := s.DeleteDirectory(context.Background(), orgID, dirID, false); !errors.Is(err, blobfs.ErrNotEmpty) {
		t.Fatalf("DeleteDirectory = %v; want ErrNotEmpty", err)
	}
}

// notEmpty is the engine's refusal of a directory's removal while a child
// directory references it, as the driver classifies it.
func notEmpty() error {
	return &sqlate.ConstraintError{Constraint: "blobfs_fk_directory_parent", Class: sqlate.ErrForeignKeyViolation, Err: errors.New("fk")}
}

// takenName is the engine's refusal of a file's insert under a name its
// directory already holds.
func takenName() error {
	return &sqlate.ConstraintError{Constraint: "blobfs_uq_file_directory_name", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}
}
