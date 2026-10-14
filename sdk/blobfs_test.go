package sdk_test

import (
	"context"
	"errors"
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
	"github.com/standards-lab/go-web-service/sdk"
)

const (
	fileID = "00000000-0000-7000-8000-00000000000f"
	dirID  = "00000000-0000-7000-8000-00000000000d"
	key    = fileID + "/report.txt"
)

const (
	q, x            = sqltest.OpQuery, sqltest.OpExec
	begin, commit   = sqltest.OpBegin, sqltest.OpCommit
	rollback        = sqltest.OpRollback
	hookStatement   = "INSERT INTO hook_rows DEFAULT VALUES"
	uploadedContent = "report"
)

// protocol is the write's dependencies over the scripted driver, blobfs's
// store in the returning dialect so each of its commands is one statement,
// and the fake object store behind the data package's adapter, started as
// the composition root starts the real one.
type protocol struct {
	st   *data.Storage
	db   *sqlate.DB
	rec  *sqltest.Recorder
	fake *storagetest.Fake
	// hooked is each file the completion hook was given.
	hooked []blobfs.File
}

func newProtocol(t *testing.T, responses ...sqltest.Response) *protocol {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	dialect := sqltest.ReturningDialect{}
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
	db := sqlate.Wrap(pool, dialect)
	return &protocol{st: data.NewStorage(data.New(db, catalog), fs, objects), db: db, rec: rec, fake: fake}
}

// write runs the staged write of the six-byte upload into the directory,
// begun by Files.Create, with a completion hook that records the file it
// is given and writes one row on the completion's transaction, failing
// with hookErr instead when that is set.
func (p *protocol) write(hookErr error) (blobfs.File, error) {
	ctx := context.Background()
	st := p.st
	return sdk.WriteFile(ctx, st.FS, p.db, st.Objects, strings.NewReader(uploadedContent), int64(len(uploadedContent)),
		func(tx *sqlate.Tx) (blobfs.File, error) {
			return st.FS.Files.Create(ctx, tx, st.Objects, dirID, "report.txt", "text/plain")
		},
		func(tx *sqlate.Tx, f blobfs.File) error {
			p.hooked = append(p.hooked, f)
			if hookErr != nil {
				return hookErr
			}
			_, err := tx.ExecContext(ctx, hookStatement)
			return err
		})
}

// stored reports whether the store holds the write's object.
func (p *protocol) stored(t *testing.T) bool {
	t.Helper()
	_, err := p.fake.Get(context.Background(), key, storage.GetOptions{})
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// sameOps fails the test unless the recorder's operations are want and
// every scripted response was consumed.
func (p *protocol) sameOps(t *testing.T, want ...sqltest.Op) {
	t.Helper()
	if got := p.rec.Ops(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", got, want)
	}
	if p.rec.Pending() != 0 || p.rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", p.rec.Pending(), p.rec.RowsLeaked())
	}
}

// file is the write's row at a status and version; one past pending
// carries the size and entity tag its completion recorded.
func file(status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: fileID, DirectoryID: dirID, Name: "report.txt", Status: status, Key: key, ContentType: "text/plain", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(len(uploadedContent)), `"etag"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

func exec(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

// abandoned scripts the abandon: the delete begun at the pending version,
// then, once the object is deleted, the row's purge.
func abandoned() []sqltest.Response {
	return []sqltest.Response{datatest.FileRows(file(blobfs.StatusDeleting, 2)), exec(1)}
}

// wantAbandoned fails the test unless the write's last calls are the
// abandon, its delete begun at the pending version, version 1.
func (p *protocol) wantAbandoned(t *testing.T) {
	t.Helper()
	calls := p.rec.Calls()
	del := calls[len(calls)-3]
	if !strings.Contains(del.SQL, "status = 'deleting'") || del.Args[1] != int64(1) {
		t.Errorf("abandon = %q %v; want the delete begun at the pending version", del.SQL, del.Args)
	}
	if p.stored(t) {
		t.Error("the abandoned write's object is still stored")
	}
}

// A write that completes runs the hook in the completion's transaction,
// after Files.Complete and before the commit, with the row available, and
// returns that row.
func TestWriteFile_TheHookRunsInTheCompletionsTransaction(t *testing.T) {
	p := newProtocol(t,
		datatest.FileRows(file(blobfs.StatusPending, 1)),
		datatest.FileRows(file(blobfs.StatusAvailable, 2)), exec(1),
	)
	done, err := p.write(nil)
	if err != nil || done.Status != blobfs.StatusAvailable || done.Version != 2 {
		t.Fatalf("WriteFile = %+v, %v; want the row available at version 2", done, err)
	}
	if len(p.hooked) != 1 || p.hooked[0].Status != blobfs.StatusAvailable || p.hooked[0].Version != 2 {
		t.Errorf("the hook was given %+v; want the available row once", p.hooked)
	}
	p.sameOps(t, begin, q, commit, begin, q, x, commit)
	if calls := p.rec.Calls(); calls[len(calls)-2].SQL != hookStatement {
		t.Errorf("the completion's transaction ran %q before its commit; want the hook's row", calls[len(calls)-2].SQL)
	}
	if !p.stored(t) {
		t.Error("the completed write's object is not stored")
	}
}

// A hook that fails fails the completion with it: the transaction rolls
// back, so the row never commits available nor the hook's writes, and the
// write is abandoned as any failed completion is.
func TestWriteFile_AFailedHookRollsBackTheCompletion(t *testing.T) {
	p := newProtocol(t, append([]sqltest.Response{
		datatest.FileRows(file(blobfs.StatusPending, 1)),
		datatest.FileRows(file(blobfs.StatusAvailable, 2)),
	}, abandoned()...)...)
	hookErr := errors.New("the hook refused")
	if _, err := p.write(hookErr); !errors.Is(err, hookErr) {
		t.Fatalf("WriteFile = %v; want the hook's error", err)
	}
	p.sameOps(t, begin, q, commit, begin, q, rollback, begin, q, commit, x)
	p.wantAbandoned(t)
}

// A put that fails abandons the write, as Store.WriteFile does, and the
// completion, with its hook, never runs.
func TestWriteFile_AFailedPutAbandons(t *testing.T) {
	p := newProtocol(t, append([]sqltest.Response{datatest.FileRows(file(blobfs.StatusPending, 1))}, abandoned()...)...)
	putErr := errors.New("put failed")
	p.fake.FailPut(putErr)
	if _, err := p.write(nil); !errors.Is(err, putErr) {
		t.Fatalf("WriteFile = %v; want the put's error", err)
	}
	if len(p.hooked) != 0 {
		t.Errorf("the hook ran over a failed put: %+v", p.hooked)
	}
	p.sameOps(t, begin, q, commit, begin, q, commit, x)
	p.wantAbandoned(t)
}

// A completion that fails for another reason than the sweep or another
// writer, here the database's, rolls back and abandons the write, and
// the hook never runs.
func TestWriteFile_AFailedCompletionAbandons(t *testing.T) {
	completeErr := errors.New("the connection broke")
	p := newProtocol(t, append([]sqltest.Response{
		datatest.FileRows(file(blobfs.StatusPending, 1)),
		{Err: completeErr},
	}, abandoned()...)...)
	if _, err := p.write(nil); !errors.Is(err, completeErr) {
		t.Fatalf("WriteFile = %v; want the completion's error", err)
	}
	if len(p.hooked) != 0 {
		t.Errorf("the hook ran over a failed completion: %+v", p.hooked)
	}
	p.sameOps(t, begin, q, commit, begin, q, rollback, begin, q, commit, x)
	p.wantAbandoned(t)
}

// A completion refused because a sweep reached the row first, which began
// its delete or removed it, deletes the object just put and abandons
// nothing, as Store.WriteFile does; the hook never runs, so nothing it
// writes commits. One that finds another writer completed the row leaves
// the object to that writer.
func TestWriteFile_ACompletionTheSweepRefused(t *testing.T) {
	// The completion's update matches no pending row, and blobfs reads the
	// row to tell why.
	cases := map[string]struct {
		reads  []sqltest.Response
		want   error
		stored bool
	}{
		"deleting": {[]sqltest.Response{
			datatest.FileRows(file(blobfs.StatusDeleting, 2)),
			datatest.DirectoryRows(blobfs.Directory{ID: dirID, Name: "reports", Status: blobfs.DirectoryStatusActive, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}),
		}, blobfs.ErrDeleting, false},
		"removed":   {[]sqltest.Response{datatest.FileRows()}, blobfs.ErrNotFound, false},
		"completed": {[]sqltest.Response{datatest.FileRows(file(blobfs.StatusAvailable, 2))}, query.ErrVersionMismatch, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := newProtocol(t, append([]sqltest.Response{
				datatest.FileRows(file(blobfs.StatusPending, 1)),
				datatest.FileRows(),
			}, c.reads...)...)
			if _, err := p.write(nil); !errors.Is(err, c.want) {
				t.Fatalf("WriteFile = %v; want %v", err, c.want)
			}
			if len(p.hooked) != 0 {
				t.Errorf("the hook ran over a refused completion: %+v", p.hooked)
			}
			want := []sqltest.Op{begin, q, commit, begin, q}
			for range c.reads {
				want = append(want, q)
			}
			p.sameOps(t, append(want, rollback)...)
			if p.stored(t) != c.stored {
				t.Errorf("object stored = %t; want %t", !c.stored, c.stored)
			}
		})
	}
}
