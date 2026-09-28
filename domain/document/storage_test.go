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
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
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
	if err := s.DeleteFile(ctx, orgID, fileID, 3); err != nil {
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

// Without recursive, a directory with contents is not empty: the foreign
// key's refusal, classified by blobfs.
func TestStore_DeleteOfANonEmptyDirectoryIsNotEmpty(t *testing.T) {
	s, _, _ := serviceOver(t, root(rootID), within(true), sqltest.Response{Err: notEmpty()})
	if err := s.DeleteDirectory(context.Background(), orgID, dirID, 1); !errors.Is(err, blobfs.ErrNotEmpty) {
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

// A recursive delete marks the branch in one transaction with its scope
// check, the directory at the If-Match version, and nudges the sweep once,
// after the commit.
func TestStore_DeleteBranchMarksThenNudges(t *testing.T) {
	s, rec, _, sweep := serviceNudged(t,
		root(rootID), within(true),
		exec(2), // the directory and one beneath it marked
		exec(3), // their files marked
	)
	id, err := s.DeleteBranch(context.Background(), orgID, dirID, 1)
	if err != nil || id != dirID {
		t.Fatalf("DeleteBranch = %q, %v", id, err)
	}
	sameOps(t, rec, begin, q, q, x, x, commit)
	mark := rec.Calls()[3]
	if !strings.HasPrefix(mark.SQL, "UPDATE blobfs_directory") || fmt.Sprint(mark.Args) != fmt.Sprint([]any{dirID, int64(1)}) {
		t.Errorf("mark = %q %v; want the directory at the If-Match version", mark.SQL, mark.Args)
	}
	if len(sweep.seen) != 1 || fmt.Sprint(sweep.seen[0]) != fmt.Sprint(rec.Ops()) {
		t.Errorf("nudges = %v; want one, after the commit", sweep.seen)
	}
}

// A repeated recursive delete, once the branch is deleting, is the mark's
// retry: at the version the client read before the mark, which the mark
// advanced, it converges and is accepted again, nudging the sweep again.
func TestStore_DeleteBranchRetryConverges(t *testing.T) {
	marked := deleting(directory(dirID, rootID, "reports", 1))
	s, rec, _, sweep := serviceNudged(t,
		root(rootID), within(true),
		exec(0), dirRows(marked), // nothing newly marked; the read finds it deleting
		exec(0),
	)
	if _, err := s.DeleteBranch(context.Background(), orgID, dirID, 1); err != nil {
		t.Fatalf("DeleteBranch = %v; want the retry accepted", err)
	}
	sameOps(t, rec, begin, q, q, x, q, x, commit)
	if len(sweep.seen) != 1 {
		t.Errorf("nudges = %d; want one", len(sweep.seen))
	}
}

// A stale version marks nothing, rolls back, and nudges no sweep; the root
// is marked like any directory, by its alias.
func TestStore_DeleteBranchAtAStaleVersion(t *testing.T) {
	s, rec, _, sweep := serviceNudged(t,
		root(rootID), within(true), exec(0), dirRows(directory(dirID, rootID, "reports", 2)),
		root(rootID), exec(1), exec(0),
	)
	ctx := context.Background()
	if _, err := s.DeleteBranch(ctx, orgID, dirID, 1); !errors.Is(err, query.ErrVersionMismatch) {
		t.Fatalf("DeleteBranch = %v; want a version mismatch", err)
	}
	if len(sweep.seen) != 0 {
		t.Errorf("nudges = %d; want none after a refused mark", len(sweep.seen))
	}
	if id, err := s.DeleteBranch(ctx, orgID, document.RootAlias, 1); err != nil || id != rootID {
		t.Fatalf("DeleteBranch(root) = %q, %v; want the root marked", id, err)
	}
	sameOps(t, rec, begin, q, q, x, q, sqltest.OpRollback, begin, q, x, x, commit)
}

// The sweep's hook removes a document root's owner row in the removal's
// transaction, and a top-level directory no row binds is success: the
// delete matches nothing. A directory below the top level cannot be a
// root, and passes without SQL.
func TestService_UnbindRootInTheRemovalsTransaction(t *testing.T) {
	s, rec, db := serviceTx(t, exec(1), exec(0))
	ctx := context.Background()
	top := directory(rootID, blobfs.RootID, orgID, 3)
	cases := []blobfs.Directory{
		top,                                    // bound: its row goes
		top,                                    // retried: no row is left, and that is success
		directory(dirID, rootID, "reports", 2), // not top-level: no SQL
	}
	for _, dir := range cases {
		_, err := db.Transact(ctx, func(tx *sqlate.Tx) (struct{}, error) {
			return struct{}{}, s.UnbindRoot(ctx, tx, dir)
		})
		if err != nil {
			t.Fatalf("UnbindRoot(%s) = %v", dir.ID, err)
		}
	}
	sameOps(t, rec, begin, x, commit, begin, x, commit, begin, commit)
	unbind := rec.Calls()[1]
	if !strings.HasPrefix(unbind.SQL, "DELETE FROM organization_directory") || fmt.Sprint(unbind.Args) != fmt.Sprint([]any{rootID}) {
		t.Errorf("unbind = %q %v; want the owner row of the removed directory", unbind.SQL, unbind.Args)
	}
}

// The deletes guard on the If-Match version: a file or an empty directory
// at another version is a version mismatch, and nothing is removed.
func TestStore_DeletesAtAStaleVersion(t *testing.T) {
	s, _, fake := serviceOver(t,
		root(rootID), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true),
		fileRows(), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), // the guarded delete matches nothing; the read tells the version
		root(rootID), within(true), exec(0), dirRows(directory(dirID, rootID, "reports", 2)),
	)
	ctx := context.Background()
	key := fileID + "/report.txt"
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFile(ctx, orgID, fileID, 2); !errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("DeleteFile = %v; want a version mismatch", err)
	}
	if _, err := fake.Get(ctx, key, storage.GetOptions{}); err != nil {
		t.Errorf("the object of a refused delete is gone: %v", err)
	}
	if err := s.DeleteDirectory(ctx, orgID, dirID, 1); !errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("DeleteDirectory = %v; want a version mismatch", err)
	}
}

// The writer rule: a completion refused because a mark reached the row,
// or because the sweep removed it, deletes the object the put stored under
// the key the write holds, and leaves the row to the sweep.
func TestStore_ACompletionTheSweepRefusedDeletesTheObject(t *testing.T) {
	// The completion's update matches no pending row, and blobfs reads the
	// row to tell why: deleting, or gone.
	cases := map[string]struct {
		read sqltest.Response
		want error
	}{
		"marked":  {fileRows(file(fileID, dirID, blobfs.StatusDeleting, 2)), blobfs.ErrDeleting},
		"removed": {fileRows(), blobfs.ErrNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, fake := serviceOver(t,
				root(rootID), within(true), fileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
				fileRows(), c.read,
			)
			_, err := s.PutFile(context.Background(), orgID, dirID, "report.txt", textUpload(t))
			if !errors.Is(err, c.want) {
				t.Fatalf("PutFile = %v; want %v", err, c.want)
			}
			if fake.Puts() != 1 {
				t.Errorf("puts = %d; want the object put once", fake.Puts())
			}
			if _, err := fake.Get(context.Background(), fileID+"/report.txt", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("the object outlived the refused completion: %v", err)
			}
			sameOps(t, rec, begin, q, q, q, commit, q, q)
		})
	}
}
