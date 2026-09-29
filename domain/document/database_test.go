package document_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/domain/document"
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

// serviceOver builds the domain over the scripted driver in the returning
// dialect, so blobfs's returning commands run as one statement each, with
// blobfs's store compiled against the same catalog and the object store
// over the fake, started as the composition root starts the real one.
func serviceOver(t *testing.T, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	s, rec, fake, _ := serviceNudged(t, responses...)
	return s, rec, fake
}

// nudges is the sweeper the tests inject: it records, at each nudge, the
// operations the scripted driver had run by then.
type nudges struct {
	rec  *sqltest.Recorder
	seen [][]sqltest.Op
}

func (n *nudges) Nudge() { n.seen = append(n.seen, n.rec.Ops()) }

// serviceNudged is serviceOver with the sweeper the service nudges.
func serviceNudged(t *testing.T, responses ...sqltest.Response) (*document.Service, *sqltest.Recorder, *storagetest.Fake, *nudges) {
	t.Helper()
	dialect := sqltest.ReturningDialect{}
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, dialect)
	if err != nil {
		t.Fatal(err)
	}
	fake := storagetest.NewFake()
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
	return document.New(db, data.NewStorage(fs, objects), sweep), rec, fake, sweep
}

// root scripts the owner row's read: the organization's document root, or
// none when no id is given.
func root(ids ...string) sqltest.Response {
	r := sqltest.Response{Columns: []string{"directory_id"}}
	for _, id := range ids {
		r.Rows = append(r.Rows, []driver.Value{id})
	}
	return r
}

// within scripts blobfs's IsWithin: whether the directory lies in the
// root's subtree.
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

// directory is an active directory row under parent at a version.
func directory(id, parent, name string, version int64) blobfs.Directory {
	now := time.Now()
	return blobfs.Directory{ID: id, ParentID: &parent, Name: name, Status: blobfs.DirectoryStatusActive, Version: version, CreatedAt: now, UpdatedAt: now}
}

// deleting is d once its branch is marked for its delete, a version on.
func deleting(d blobfs.Directory) blobfs.Directory {
	d.Status = blobfs.DirectoryStatusDeleting
	d.Version++
	return d
}

// file is a document's file row in dir at a status and version; one past
// pending carries the size and the entity tag its completion recorded.
func file(id, dir string, status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: id, DirectoryID: dir, Name: "report.txt", Status: status, Key: id + "/report.txt", ContentType: "text/plain", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(6), `"etag-`+id+`"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

// The wiring test: every owner-row handle binds once with its arguments,
// through the protocol that runs them all, so a key that does not
// match its file's parameters or a scan out of step with the SELECT list
// fails here rather than on a request. The strict driver checks the
// placeholder count on every call.
func TestStore_EveryHandleBindsItsFilesParameters(t *testing.T) {
	ctx := context.Background()
	s, rec, _ := serviceOver(t,
		// CreateDirectory under the alias, the organization's first write.
		root(),                   // no root yet
		organization(),           // ensure: the organization exists
		datatest.DirectoryRows(), // ensure: no directory named for it
		datatest.DirectoryRows(directory(rootID, blobfs.RootID, orgID, 1)), // ensure: the root created
		exec(1),      // ensure: the owner row
		root(rootID), // create: the scope check
		datatest.DirectoryRows(directory(dirID, rootID, "reports", 1)), // create
		// DeleteDirectory of the root, empty: its owner row goes with it
		// through the cascading foreign key, so no statement names the row.
		root(rootID),
		exec(1), // the root removed, on the pool
	)
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
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpExec, sqltest.OpCommit,
		sqltest.OpBegin, sqltest.OpQuery, sqltest.OpQuery, sqltest.OpCommit,
		sqltest.OpQuery, sqltest.OpExec,
	}
	if len(ops) != len(want) {
		t.Fatalf("ops = %v\nwant %v", ops, want)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("ops = %v\nwant %v", ops, want)
		}
	}
}

// Verify prepares the layer's four statements and nothing of blobfs's,
// which the composition root verifies on its own.
func TestStore_VerifyPreparesEveryStatement(t *testing.T) {
	s, rec, _ := serviceOver(t)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prepared := rec.SQL(sqltest.OpPrepare); len(prepared) != 4 {
		t.Errorf("prepared %d statements, want 4: %q", len(prepared), prepared)
	}
}
