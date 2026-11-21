package document_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
)

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
	rollback      = sqltest.OpRollback
)

func TestStore_FileProtocols(t *testing.T) {
	ctx := context.Background()
	pendingRowWithItsScope := []sqltest.Response{root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1))}
	completionWithItsEvent := []sqltest.Response{datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), written()}
	content := []sqltest.Response{root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true)}
	moveToTheRootByItsAlias := []sqltest.Response{
		root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true),
		datatest.FileRows(file(fileID, rootID, blobfs.StatusAvailable, 3)), written(),
	}
	deleteBegunWithItsEventThenPurged := []sqltest.Response{
		root(rootID), datatest.FileRows(file(fileID, rootID, blobfs.StatusAvailable, 3)),
		datatest.FileRows(file(fileID, rootID, blobfs.StatusDeleting, 4)), written(),
		purged(),
	}
	s, rec, fake := serviceOver(t, slices.Concat(pendingRowWithItsScope, completionWithItsEvent, content, moveToTheRootByItsAlias, deleteBegunWithItsEventThenPurged)...)
	id, err := s.UploadFile(ctx, orgID, dirID, "report.txt", textUpload(t))
	if err != nil || id != (document.Identity{ID: fileID, Version: 2}) {
		t.Fatalf("UploadFile = %+v, %v", id, err)
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
		begin, q, q, q, commit, begin, q, x, commit,
		q, q, q,
		begin, q, q, q, q, x, commit,
		begin, q, q, q, x, commit, x,
	)
}

func TestStore_UploadFileEnsuresTheRoot(t *testing.T) {
	s, rec, _ := serviceOver(t,
		root(), organization(), datatest.DirectoryRows(), datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)), exec(1), exec(1),
		root(rootID), datatest.FileRows(file(fileID, rootID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, rootID, blobfs.StatusAvailable, 2)), exec(1),
	)
	if _, err := s.UploadFile(context.Background(), orgID, document.RootAlias, "report.txt", textUpload(t)); err != nil {
		t.Fatalf("UploadFile = %v", err)
	}
	sameOps(t, rec,
		q, begin, q, q, q, x, x, commit,
		begin, q, q, commit, begin, q, x, commit,
	)
}

func TestStore_EnsureRootForAMissingOrganizationIsNoRows(t *testing.T) {
	s, rec, _ := serviceOver(t, root(), sqltest.Response{Columns: []string{"id"}})
	_, err := s.CreateDirectory(context.Background(), orgID, document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"})
	if err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("CreateDirectory = %v; want the missing organization", err)
	}
	sameOps(t, rec, q, begin, q, sqltest.OpRollback)
}

func TestStore_AFailedPutRetiresThePendingRow(t *testing.T) {
	s, rec, fake := serviceOver(t,
		root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 2)), exec(1),
	)
	fake.FailPut(errors.New("put failed"))
	if _, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t)); err == nil {
		t.Fatal("UploadFile succeeded over a failed put")
	}
	sameOps(t, rec, begin, q, q, q, commit, begin, q, commit, x)
	noEvent(t, rec)
}

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
		root(rootID), within(true), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), ancestors,
		root(rootID), datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)),
		sqltest.Response{Columns: []string{"id", "parent_id", "name"}, Rows: [][]driver.Value{{rootID, blobfs.RootID, orgID}, {blobfs.RootID, nil, "/"}}},
		root(rootID), sqltest.WithTotal(datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), 1),
		datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)),
		root(rootID), within(true), sqltest.WithTotal(datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), 1),
		datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)),
	)
	d, err := s.Directory(ctx, orgID, dirID)
	if err != nil || d.Path != "/reports" || d.Name != "reports" || *d.ParentID != rootID || d.Status != document.DirectoryActive {
		t.Fatalf("Directory = %+v, %v", d, err)
	}
	r, err := s.Directory(ctx, orgID, document.RootAlias)
	if err != nil || r.Path != "/" || r.Name != "/" || r.ParentID != nil || r.ID != rootID {
		t.Fatalf("Directory(root) = %+v, %v", r, err)
	}
	if ops := fmt.Sprint(rec.Ops()); ops != fmt.Sprint([]sqltest.Op{begin, q, q, q, q, commit, begin, q, q, q, commit}) {
		t.Errorf("metadata reads = %v; want each in its own transaction", ops)
	}
	limits := web.Limits{DefaultSize: 20, MaxSize: 100, Cursor: true}
	query, _ := web.ParseQuery(url.Values{"sort": {"-created_at"}}, limits)
	dirs, paging, err := s.ListDirectories(ctx, orgID, document.RootAlias, query)
	if err != nil || len(dirs) != 1 || dirs[0].ID != dirID || (paging.Total == nil || *paging.Total != 1) || paging.More {
		t.Fatalf("ListDirectories = %+v, %+v, %v", dirs, paging, err)
	}
	query, _ = web.ParseQuery(url.Values{"status": {"available"}}, limits)
	files, paging, err := s.ListFiles(ctx, orgID, dirID, query)
	if err != nil || len(files) != 1 || files[0].Status != document.FileAvailable || (paging.Total == nil || *paging.Total != 1) {
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

func TestStore_ADeletingDirectory(t *testing.T) {
	ctx := context.Background()
	marked := deleting(directory(dirID, rootID, "reports", 1))
	s, _, _ := serviceOver(t,
		root(rootID), within(true), datatest.DirectoryRows(marked),
		sqltest.Response{
			Columns: []string{"id", "parent_id", "name"},
			Rows:    [][]driver.Value{{dirID, rootID, "reports"}, {rootID, blobfs.RootID, orgID}, {blobfs.RootID, nil, "/"}},
		},
		root(rootID), within(true), sqltest.WithTotal(datatest.DirectoryRows(), 0), datatest.DirectoryRows(marked),
		root(rootID), within(true), sqltest.WithTotal(datatest.FileRows(), 0), datatest.DirectoryRows(marked),
	)
	d, err := s.Directory(ctx, orgID, dirID)
	if err != nil || d.Status != document.DirectoryDeleting || d.Version != 2 {
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

func TestStore_ReadsBeforeTheRoot(t *testing.T) {
	ctx := context.Background()
	empty := func() []sqltest.Response {
		return []sqltest.Response{root(), organization(), sqltest.WithTotal(datatest.FileRows(), 0), datatest.DirectoryRows()}
	}
	responses := append(empty(), empty()...)
	responses = append(responses, root(), organization(), root(), organization(), root())
	s, rec, _ := serviceOver(t, responses...)
	limits := web.Limits{DefaultSize: 20, MaxSize: 100, Cursor: true}
	parse := func(v url.Values) web.Query {
		t.Helper()
		q, err := web.ParseQuery(v, limits)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	items, paging, err := s.ListFiles(ctx, orgID, document.RootAlias, parse(url.Values{}))
	if err != nil || len(items) != 0 || (paging.Total == nil || *paging.Total != 0) {
		t.Fatalf("ListFiles(root) = %v, %+v, %v; want an empty page", items, paging, err)
	}
	if list := rec.Calls()[2]; slices.Contains(list.Args, any(blobfs.RootID)) {
		t.Errorf("the alias's listing = %q %v; want it anchored on a fresh id, never blobfs's root", list.SQL, list.Args)
	}
	items, paging, err = s.ListFiles(ctx, orgID, document.RootAlias, parse(url.Values{"page": {"2"}}))
	if err != nil || len(items) != 0 || paging.Total != nil {
		t.Errorf("ListFiles(root, page 2) = %v, %+v, %v; want an empty page with no total", items, paging, err)
	}
	if _, _, err := s.ListFiles(ctx, orgID, document.RootAlias, parse(url.Values{"sort": {"nope"}})); !errors.Is(err, query.ErrDirectives) {
		t.Errorf("ListFiles(root, sort=nope) = %v; want the directives refused", err)
	}
	var cursor *query.CursorError
	if _, _, err := s.ListFiles(ctx, orgID, document.RootAlias, parse(url.Values{"cursor": {"past"}})); !errors.As(err, &cursor) {
		t.Errorf("ListFiles(root, cursor=past) = %v; want the cursor refused", err)
	}
	if _, _, err := s.ListFiles(ctx, orgID, dirID, parse(url.Values{})); err == nil {
		t.Fatal("ListFiles(id) before the root found a directory")
	}
}

func TestStore_ReadsOfAMissingOrganization(t *testing.T) {
	ctx := context.Background()
	s, rec, _ := serviceOver(t,
		root(), sqltest.Response{Columns: []string{"id"}},
		root(), sqltest.Response{Columns: []string{"id"}},
	)
	page, _ := web.ParseQuery(url.Values{}, web.Limits{DefaultSize: 20, MaxSize: 100})
	if _, _, err := s.ListFiles(ctx, orgID, document.RootAlias, page); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ListFiles(root) = %v; want the missing organization", err)
	}
	if _, _, err := s.ListDirectories(ctx, orgID, document.RootAlias, page); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ListDirectories(root) = %v; want the missing organization", err)
	}
	sameOps(t, rec, q, q, q, q)
}

func TestStore_MoveDirectory(t *testing.T) {
	directoryInScope, destinationInScope, noCycle := within(true), within(true), within(false)
	guardedUpdate := datatest.DirectoryRows(directory(subID, dirID, "archive", 2))
	s, rec, _ := serviceOver(t,
		root(rootID), directoryInScope, destinationInScope,
		noCycle,
		guardedUpdate,
		written(),
	)
	id, err := s.MoveDirectory(context.Background(), orgID, subID, 1, document.MoveDirectory{ParentID: dirID, Name: "archive"})
	if err != nil || id != (document.Identity{ID: subID, Version: 2}) {
		t.Fatalf("MoveDirectory = %+v, %v", id, err)
	}
	sameOps(t, rec, begin, q, q, q, q, q, x, commit)
}

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

func TestStore_MoveOutsideTheRootIsNotFound(t *testing.T) {
	directoryInScope, destinationOutside := within(true), within(false)
	s, rec, _ := serviceOver(t,
		root(rootID), directoryInScope, destinationOutside,
		root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)), within(true), within(false),
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

func TestStore_DeleteOfANonEmptyDirectoryIsNotEmpty(t *testing.T) {
	s, _, _ := serviceOver(t, root(rootID), within(true), sqltest.Response{Err: notEmpty()})
	if err := s.DeleteDirectory(context.Background(), orgID, dirID, 1); !errors.Is(err, blobfs.ErrNotEmpty) {
		t.Fatalf("DeleteDirectory = %v; want ErrNotEmpty", err)
	}
}

func notEmpty() error {
	return &sqlate.ConstraintError{Constraint: "blobfs_fk_directory_parent", Class: sqlate.ErrForeignKeyViolation, Err: errors.New("fk")}
}

func takenName() error {
	return &sqlate.ConstraintError{Constraint: "blobfs_uq_file_directory_name", Class: sqlate.ErrUniqueViolation, Err: errors.New("unique")}
}

func TestStore_DeleteBranchMarksThenNudges(t *testing.T) {
	readActiveBeforeItsMark := datatest.DirectoryRows(directory(dirID, rootID, "reports", 1))
	readForItsEvent := datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1)))
	s, rec, _, sweep := serviceOnPostgres(t,
		root(rootID), within(true), treeLocked(),
		readActiveBeforeItsMark,
		treeLocked(),
		directoriesMarked(2),
		filesMarked(3),
		readForItsEvent, written(),
	)
	id, err := s.DeleteBranch(context.Background(), orgID, dirID, 1)
	if err != nil || id != dirID {
		t.Fatalf("DeleteBranch = %q, %v", id, err)
	}
	sameOps(t, rec, begin, q, q, x, q, x, x, x, q, x, commit)
	calls := rec.Calls()
	for _, i := range []int{3, 5} {
		if !strings.Contains(calls[i].SQL, "pg_advisory_xact_lock") {
			t.Errorf("call %d = %q; want the tree lock", i, calls[i].SQL)
		}
	}
	mark := calls[6]
	if !strings.HasPrefix(mark.SQL, "UPDATE blobfs_directory") || fmt.Sprint(mark.Args) != fmt.Sprint([]any{dirID, int64(1)}) {
		t.Errorf("mark = %q %v; want the directory at the If-Match version", mark.SQL, mark.Args)
	}
	if len(sweep.seen) != 1 || fmt.Sprint(sweep.seen[0]) != fmt.Sprint(rec.Ops()) {
		t.Errorf("nudges = %v; want one, after the commit", sweep.seen)
	}
}

func TestStore_DeleteBranchRetryConverges(t *testing.T) {
	marked := deleting(directory(dirID, rootID, "reports", 1))
	s, rec, _, sweep := serviceOnPostgres(t,
		root(rootID), within(true), exec(0), datatest.DirectoryRows(marked),
		treeLocked(), directoriesMarked(0), datatest.DirectoryRows(marked),
		exec(0),
	)
	if _, err := s.DeleteBranch(context.Background(), orgID, dirID, 1); err != nil {
		t.Fatalf("DeleteBranch = %v; want the retry accepted", err)
	}
	sameOps(t, rec, begin, q, q, x, q, x, x, q, x, commit)
	if len(sweep.seen) != 1 {
		t.Errorf("nudges = %d; want one", len(sweep.seen))
	}
}

func TestStore_DeleteBranchAtAStaleVersion(t *testing.T) {
	s, rec, _, sweep := serviceOnPostgres(t,
		root(rootID), within(true), exec(0), datatest.DirectoryRows(directory(dirID, rootID, "reports", 2)), exec(0), exec(0), datatest.DirectoryRows(directory(dirID, rootID, "reports", 2)),
		root(rootID), exec(0), datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)), exec(0), exec(1), exec(0), datatest.DirectoryRows(deleting(directory(rootID, blobfs.RootID, orgID, 1))), exec(1),
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
	sameOps(t, rec, begin, q, q, x, q, x, x, q, sqltest.OpRollback, begin, q, x, q, x, x, x, q, x, commit)
}

func TestStore_DeletesAtAStaleVersion(t *testing.T) {
	s, _, fake := serviceOver(t,
		root(rootID), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true),
		guardedUpdateMatchesNothing(), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)),
		root(rootID), within(true), exec(0), datatest.DirectoryRows(directory(dirID, rootID, "reports", 2)),
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

func TestStore_ACompletionTheSweepRefusedDeletesTheObject(t *testing.T) {
	deletingRow := datatest.FileRows(file(fileID, dirID, blobfs.StatusDeleting, 2))
	cases := map[string]struct {
		reads           []sqltest.Response
		want            error
		directoryDelete *bool
	}{
		"marked":    {[]sqltest.Response{deletingRow, datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1)))}, blobfs.ErrDeleting, new(true)},
		"reclaimed": {[]sqltest.Response{deletingRow, datatest.DirectoryRows(directory(dirID, rootID, "reports", 1))}, blobfs.ErrDeleting, new(false)},
		"removed":   {[]sqltest.Response{datatest.FileRows()}, blobfs.ErrNotFound, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, fake := serviceOver(t, append([]sqltest.Response{
				root(rootID), within(true), datatest.FileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
				guardedUpdateMatchesNothing(),
			}, c.reads...)...)
			_, err := s.UploadFile(context.Background(), orgID, dirID, "report.txt", textUpload(t))
			if !errors.Is(err, c.want) {
				t.Fatalf("UploadFile = %v; want %v", err, c.want)
			}
			wantDeleting(t, err, c.directoryDelete)
			if fake.Puts() != 1 {
				t.Errorf("puts = %d; want the object put once", fake.Puts())
			}
			if _, err := fake.Get(context.Background(), fileID+"/report.txt", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("the object outlived the refused completion: %v", err)
			}
			want := []sqltest.Op{begin, q, q, q, commit, begin, q}
			for range c.reads {
				want = append(want, q)
			}
			sameOps(t, rec, append(want, rollback)...)
			noEvent(t, rec)
		})
	}
}

func wantDeleting(t *testing.T, err error, directory *bool) {
	t.Helper()
	var de *blobfs.DeletingError
	switch {
	case directory == nil && errors.As(err, &de):
		t.Errorf("err = %v; want no DeletingError", err)
	case directory != nil && !errors.As(err, &de):
		t.Errorf("err = %v; want a DeletingError", err)
	case directory != nil && de.Directory != *directory:
		t.Errorf("DeletingError = %+v; want Directory %t", de, *directory)
	}
}

func TestStore_AMoveOfADeletingFile(t *testing.T) {
	deletingFile := file(fileID, dirID, blobfs.StatusDeleting, 3)
	activeDirectory := datatest.DirectoryRows(directory(dirID, rootID, "reports", 1))
	deletingDestination := datatest.DirectoryRows(deleting(directory(subID, rootID, "archive", 1)))
	scope := func(f blobfs.File) []sqltest.Response {
		return []sqltest.Response{root(rootID), datatest.FileRows(f), within(true), within(true)}
	}
	cases := map[string]struct {
		responses []sqltest.Response
		directory bool
	}{
		"its own delete": {append(scope(deletingFile),
			guardedUpdateMatchesNothing(), datatest.FileRows(deletingFile),
			activeDirectory,
		), false},
		"a marked branch": {append(scope(deletingFile),
			guardedUpdateMatchesNothing(), datatest.FileRows(deletingFile),
			datatest.DirectoryRows(deleting(directory(dirID, rootID, "reports", 1))),
		), true},
		"a deleting destination": {append(scope(file(fileID, dirID, blobfs.StatusAvailable, 2)),
			guardedUpdateMatchesNothing(), datatest.FileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)),
			activeDirectory,
			deletingDestination,
		), true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec, _ := serviceOver(t, c.responses...)
			_, err := s.MoveFile(context.Background(), orgID, fileID, 2, document.MoveFile{DirectoryID: subID, Name: "report.txt"})
			if !errors.Is(err, blobfs.ErrDeleting) {
				t.Fatalf("MoveFile = %v; want blobfs.ErrDeleting", err)
			}
			wantDeleting(t, err, &c.directory)
			if rec.Pending() != 0 {
				t.Errorf("pending = %d: %v", rec.Pending(), rec.Ops())
			}
		})
	}
}
