package document_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/blobfs/data/datatest"
	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-messaging/outbox"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

const (
	orgID   = "00000000-0000-7000-8000-000000000000"
	rootID  = "00000000-0000-7000-8000-0000000000a0"
	dirID   = "00000000-0000-7000-8000-0000000000b0"
	subID   = "00000000-0000-7000-8000-0000000000c0"
	otherID = "00000000-0000-7000-8000-0000000000e0"
	fileID  = "00000000-0000-7000-8000-0000000000f0"
	file2ID = "00000000-0000-7000-8000-0000000000f1"
)

func serviceOver(t *testing.T, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	s, rec, fake, _ := serviceNudged(t, responses...)
	return s, rec, fake
}

type nudges struct {
	rec  *sqltest.Recorder
	seen [][]sqltest.Op
}

func (n *nudges) Nudge() { n.seen = append(n.seen, n.rec.Ops()) }

func serviceNudged(t *testing.T, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake, *nudges) {
	t.Helper()
	return serviceOn(t, bfdata.Options{}, responses...)
}

func serviceOnPostgres(t *testing.T, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake, *nudges) {
	t.Helper()
	return serviceOn(t, bfdata.Options{Engine: blobfspg.Engine}, responses...)
}

func serviceOn(t *testing.T, opts bfdata.Options, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake, *nudges) {
	t.Helper()
	dialect := sqltest.ReturningDialect{}
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, dialect, opts)
	if err != nil {
		t.Fatal(err)
	}
	fake := storagetest.NewFake(storagetest.Options{})
	cfg := storage.Config{Container: "test"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	objects := storage.New(fake, cfg)
	if err := objects.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Shutdown(context.Background()) })
	db := data.New(sqlate.Wrap(pool, dialect), catalog)
	sweep := &nudges{rec: rec}
	return document.New(db, data.NewStorage(db, fs, objects), sweep, recorder(t)), rec, fake, sweep
}

func recorder(t *testing.T) *document.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return outboxtest.Stamped(ob.Sink())
}

func root(ids ...string) sqltest.Response {
	r := sqltest.Response{Columns: []string{"directory_id"}}
	for _, id := range ids {
		r.Rows = append(r.Rows, []driver.Value{id})
	}
	return r
}

func within(in bool) sqltest.Response {
	n := int64(0)
	if in {
		n = 1
	}
	return sqltest.Response{Columns: []string{"matches"}, Rows: [][]driver.Value{{n}}}
}

func organization() sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{orgID}}}
}

func exec(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

func treeLocked() sqltest.Response { return exec(0) }

func directoriesMarked(n int64) sqltest.Response { return exec(n) }

func filesMarked(n int64) sqltest.Response { return exec(n) }

func purged() sqltest.Response { return exec(1) }

func ownerRowBound() sqltest.Response { return exec(1) }

func rootRemoved() sqltest.Response { return exec(1) }

func guardedUpdateMatchesNothing() sqltest.Response { return datatest.FileRows() }

func directory(id, parent, name string, version int64) blobfs.Directory {
	now := time.Now()
	return blobfs.Directory{ID: id, ParentID: &parent, Name: name, Status: blobfs.DirectoryStatusActive, Version: version, CreatedAt: now, UpdatedAt: now}
}

func deleting(d blobfs.Directory) blobfs.Directory {
	d.Status = blobfs.DirectoryStatusDeleting
	d.Version++
	return d
}

func file(id, dir string, status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: id, DirectoryID: dir, Name: "report.txt", Status: status, Key: id + "/report.txt", ContentType: "text/plain", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(6), `"etag-`+id+`"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

func TestStore_EveryHandleBindsItsFilesParameters(t *testing.T) {
	ctx := context.Background()
	noRootYet := root()
	ensureRoot := []sqltest.Response{
		organization(),
		datatest.DirectoryRows(),
		datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)),
		ownerRowBound(),
		written(),
	}
	createUnderTheAlias := []sqltest.Response{root(rootID), datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), written()}
	deleteTheEmptyRootItsOwnerRowCascading := []sqltest.Response{root(rootID), rootRemoved(), written()}
	s, rec, _ := serviceOver(t, slices.Concat([]sqltest.Response{noRootYet}, ensureRoot, createUnderTheAlias, deleteTheEmptyRootItsOwnerRowCascading)...)
	id, err := s.CreateDirectory(ctx, orgID, document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"})
	if err != nil || id != (document.Identity{ID: dirID, Version: 1}) {
		t.Fatalf("CreateDirectory = %+v, %v", id, err)
	}
	if err := s.DeleteDirectory(ctx, orgID, document.RootAlias, 1); err != nil {
		t.Fatalf("DeleteDirectory = %v", err)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
	calls := rec.Calls()
	var bind, remove sqltest.Call
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c.SQL, "INSERT INTO organization_directory"):
			bind = c
		case strings.HasPrefix(c.SQL, "DELETE FROM organization_directory"):
			t.Errorf("unbind = %q; want the owner row left to the cascade", c.SQL)
		case strings.HasPrefix(c.SQL, "DELETE FROM blobfs_directory"):
			remove = c
		}
	}
	if len(bind.Args) != 2 || bind.Args[0] != rootID || bind.Args[1] != orgID {
		t.Errorf("bind = %+v; want the root bound to the organization", bind)
	}
	if fmt.Sprint(remove.Args) != fmt.Sprint([]any{rootID, int64(1)}) {
		t.Errorf("remove = %+v; want the root at the If-Match version", remove)
	}
	ops := rec.Ops()
	want := []sqltest.Op{
		sqltest.OpQuery,
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpExec, sqltest.OpExec, sqltest.OpCommit,
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpExec, sqltest.OpCommit,
	}
	deleteScopedInItsTransaction := []sqltest.Op{sqltest.OpBegin, sqltest.OpQuery, sqltest.OpExec, sqltest.OpExec, sqltest.OpCommit}
	want = append(want, deleteScopedInItsTransaction...)
	if len(ops) != len(want) {
		t.Fatalf("ops = %v\nwant %v", ops, want)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("ops = %v\nwant %v", ops, want)
		}
	}
}

func TestSeed_VerifiesTheRegisteredStores(t *testing.T) {
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{}, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	alone, blobfsRec := sqltest.Open(t)
	if err := fs.Verify(context.Background(), sqlate.Wrap(alone, sqltest.Dialect{})); err != nil {
		t.Fatal(err)
	}
	pool, rec := sqltest.Open(t)
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog)
	svc := document.New(db, data.NewStorage(db, fs, nil), nil, nil)
	if err := data.NewSeeder(db, svc.Seed()).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	for _, entry := range db.Registry() {
		for _, st := range entry.Statements.Statements() {
			if !slices.Contains(prepared, st.Text()) {
				t.Errorf("%s's statement %s was not verified", entry.Name, st.Name())
			}
		}
	}
	for _, sql := range blobfsRec.SQL(sqltest.OpPrepare) {
		if !slices.Contains(prepared, sql) {
			t.Errorf("blobfs's statement %q was not verified", sql)
		}
	}
}
