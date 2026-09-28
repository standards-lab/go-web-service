package data_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

const (
	fileID = "00000000-0000-7000-8000-00000000000f"
	dirID  = "00000000-0000-7000-8000-00000000000d"
	key    = fileID + "/report.txt"
)

const (
	q, x                    = sqltest.OpQuery, sqltest.OpExec
	begin, commit, rollback = sqltest.OpBegin, sqltest.OpCommit, sqltest.OpRollback
)

// protocols builds Storage over the scripted driver, blobfs's store in the
// returning dialect so each of its commands is one statement, and the fake
// object store, started as the composition root starts the real one.
func protocols(t *testing.T, responses ...sqltest.Response) (*data.Storage, *sqlate.DB, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	dialect := sqltest.ReturningDialect{}
	fs, err := bfdata.New(query.MustCatalog(query.Patterns(), bfdata.Patterns()), dialect)
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
	return data.NewStorage(fs, objects), sqlate.Wrap(pool, dialect), rec, fake
}

// file is the protocol's file row at a status and version; an available
// one carries the size and entity tag its completion recorded.
func file(status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: fileID, DirectoryID: dirID, Name: "report.txt", Status: status, Key: key, ContentType: "text/plain", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(6), `"etag"`
		f.Size, f.ETag = &size, &etag
	}
	return f
}

// fileRows scripts a read of blobfs file rows.
func fileRows(files ...blobfs.File) sqltest.Response {
	r := sqltest.Response{Columns: []string{"id", "directory_id", "name", "status", "key", "size", "content_type", "etag", "version", "created_at", "updated_at"}}
	for _, f := range files {
		var size, etag driver.Value
		if f.Size != nil {
			size = *f.Size
		}
		if f.ETag != nil {
			etag = *f.ETag
		}
		r.Rows = append(r.Rows, []driver.Value{f.ID, f.DirectoryID, f.Name, string(f.Status), f.Key, size, f.ContentType, etag, f.Version, f.CreatedAt, f.UpdatedAt})
	}
	return r
}

func exec(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

// sameOps fails the test unless the recorder's operations are want and
// every scripted response was consumed.
func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", got, want)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
}

// create is a write's first step as a domain's begin runs it: a check of
// its own, then blobfs's create.
func create(ctx context.Context, st *data.Storage, own func(*sqlate.Tx) error) func(*sqlate.Tx) (blobfs.File, error) {
	return func(tx *sqlate.Tx) (blobfs.File, error) {
		if err := own(tx); err != nil {
			return blobfs.File{}, err
		}
		return st.FS.Files.Create(ctx, tx, st.Objects, dirID, "report.txt", "text/plain")
	}
}

func pass(*sqlate.Tx) error { return nil }

func stored(t *testing.T, fake *storagetest.Fake) bool {
	t.Helper()
	_, err := fake.Get(context.Background(), key, storage.GetOptions{})
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// The write: begin's transaction with the pending row, the put under the
// row's key in the type the row declares, the completion on the pool.
func TestWrite_StoresAndCompletes(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(file(blobfs.StatusPending, 1)), fileRows(file(blobfs.StatusAvailable, 2)),
	)
	got, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, pass))
	if err != nil || got.Status != blobfs.StatusAvailable || got.Version != 2 {
		t.Fatalf("Write = %+v, %v", got, err)
	}
	if opts, n := fake.LastPut(); opts.ContentType != "text/plain" || n != 6 {
		t.Errorf("put = %+v, %d bytes; want the row's type and the body", opts, n)
	}
	if !stored(t, fake) {
		t.Error("the object was not stored under the row's key")
	}
	sameOps(t, rec, begin, q, commit, q)
}

// A begin that fails rolls back and stores nothing.
func TestWrite_ARefusedBeginStoresNothing(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t)
	refused := errors.New("out of scope")
	if _, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, func(*sqlate.Tx) error { return refused })); !errors.Is(err, refused) {
		t.Fatalf("Write = %v; want begin's error", err)
	}
	if fake.Puts() != 0 {
		t.Errorf("puts = %d; want none", fake.Puts())
	}
	sameOps(t, rec, begin, rollback)
}

// A failed put abandons the write through the delete protocol: the row's
// delete begun, the object delete, the purge.
func TestWrite_AFailedPutAbandonsTheRow(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(file(blobfs.StatusPending, 1)),
		fileRows(file(blobfs.StatusDeleting, 2)), exec(1),
	)
	failed := errors.New("put failed")
	fake.FailPut(failed)
	if _, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, pass)); !errors.Is(err, failed) {
		t.Fatalf("Write = %v; want the put's error", err)
	}
	sameOps(t, rec, begin, q, commit, begin, q, commit, x)
	if del := rec.Calls()[4]; !strings.HasPrefix(del.SQL, "UPDATE blobfs_file") || fmt.Sprint(del.Args) != fmt.Sprint([]any{fileID, nil}) {
		t.Errorf("delete = %q %v; want the pending row's, at no version", del.SQL, del.Args)
	}
}

// An abandon the store refuses too leaves the row deleting, a stale row
// for the sweep, and reports both failures.
func TestWrite_AnAbandonTheStoreRefusesLeavesTheRowDeleting(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(file(blobfs.StatusPending, 1)),
		fileRows(file(blobfs.StatusDeleting, 2)),
	)
	fake.Down.Store(true)
	if _, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, pass)); !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Write = %v; want the store's outage", err)
	}
	sameOps(t, rec, begin, q, commit, begin, q, commit)
}

// The writer rule: a completion refused because the row is deleting or
// gone deletes the object under the key the write holds and leaves the row.
func TestWrite_ARefusedCompletionDeletesTheObject(t *testing.T) {
	cases := map[string]struct {
		read sqltest.Response
		want error
	}{
		"deleting": {fileRows(file(blobfs.StatusDeleting, 2)), blobfs.ErrDeleting},
		"removed":  {fileRows(), blobfs.ErrNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st, db, rec, fake := protocols(t, fileRows(file(blobfs.StatusPending, 1)), fileRows(), c.read)
			if _, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, pass)); !errors.Is(err, c.want) {
				t.Fatalf("Write = %v; want %v", err, c.want)
			}
			if fake.Puts() != 1 || stored(t, fake) {
				t.Errorf("puts = %d, stored = %v; want the object put once, then deleted", fake.Puts(), stored(t, fake))
			}
			sameOps(t, rec, begin, q, commit, q, q)
		})
	}
}

// A completion that fails any other way abandons the write, its object
// deleted with the row.
func TestWrite_AFailedCompletionAbandonsTheWrite(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(file(blobfs.StatusPending, 1)),
		sqltest.Response{Err: sqlate.ErrConnectionFailed},
		fileRows(file(blobfs.StatusDeleting, 2)), exec(1),
	)
	if _, err := st.Write(ctx, db, strings.NewReader("report"), 6, create(ctx, st, pass)); !errors.Is(err, sqlate.ErrConnectionFailed) {
		t.Fatalf("Write = %v; want the completion's error", err)
	}
	if stored(t, fake) {
		t.Error("the object outlived the abandoned write")
	}
	sameOps(t, rec, begin, q, commit, q, begin, q, commit, x)
}

// ensure is a seed's first step as its begin runs it: blobfs's
// insert-or-find of the file under the seed's fixed id.
func ensure(ctx context.Context, st *data.Storage) func(*sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
	return func(tx *sqlate.Tx) (blobfs.File, bfdata.WriteOutcome, error) {
		return st.FS.Files.Ensure(ctx, tx, st.Objects, dirID, "report.txt", "text/plain", bfdata.WithID(fileID))
	}
}

// The retry-safe write: a name no row holds is created under the fixed id,
// then written as Write writes it. An object a reset left at the key is
// replaced whole by the put, so the write completes over it.
func TestEnsure_CreatesAndStoresOverALeftoverObject(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(), // the lookup finds no row by the name
		fileRows(file(blobfs.StatusPending, 1)), fileRows(file(blobfs.StatusAvailable, 2)),
	)
	if _, err := fake.Put(ctx, key, strings.NewReader("stale bytes"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	got, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st))
	if err != nil || !stored || got.Status != blobfs.StatusAvailable {
		t.Fatalf("Ensure = %+v, %v, %v; want the file stored", got, stored, err)
	}
	if insert := rec.Calls()[2]; !strings.HasPrefix(insert.SQL, "INSERT INTO blobfs_file") || insert.Args[0] != fileID || insert.Args[2] != key {
		t.Errorf("insert = %q %v; want the fixed id and its key", insert.SQL, insert.Args)
	}
	blob, err := fake.Get(ctx, key, storage.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blob.Body.Close() }()
	if b, _ := io.ReadAll(blob.Body); string(b) != "report" {
		t.Errorf("object = %q; want the leftover replaced by the write's bytes", b)
	}
	sameOps(t, rec, begin, q, q, commit, q)
}

// A pending row an earlier write left is resumed: its object put under its
// key and the row completed at its version.
func TestEnsure_ResumesAPendingRow(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(file(blobfs.StatusPending, 1)), fileRows(file(blobfs.StatusAvailable, 2)),
	)
	if _, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st)); err != nil || !stored {
		t.Fatalf("Ensure = %v, %v; want the pending row completed", stored, err)
	}
	if fake.Puts() != 1 {
		t.Errorf("puts = %d; want one", fake.Puts())
	}
	sameOps(t, rec, begin, q, commit, q)
}

// An available row is left as it stands, nothing put; a deleting one is
// refused, its delete under way.
func TestEnsure_APresentRowIsLeftAlone(t *testing.T) {
	cases := map[string]struct {
		status blobfs.Status
		want   error
	}{
		"available": {blobfs.StatusAvailable, nil},
		"deleting":  {blobfs.StatusDeleting, blobfs.ErrDeleting},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			st, db, rec, fake := protocols(t, fileRows(file(c.status, 2)))
			got, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st))
			if !errors.Is(err, c.want) || stored || (c.want == nil && got.ID != fileID) {
				t.Fatalf("Ensure = %+v, %v, %v; want the row as it stands, %v", got, stored, err, c.want)
			}
			if fake.Puts() != 0 {
				t.Errorf("puts = %d; want none", fake.Puts())
			}
			sameOps(t, rec, begin, q, commit)
		})
	}
}

// Two writers share a row once its pending insert commits: a completion
// the other writer won is no failure, abandons nothing, and returns the
// row the other completed as found.
func TestEnsure_ACompletionAnotherWriterWonIsFound(t *testing.T) {
	ctx := context.Background()
	st, db, rec, _ := protocols(t,
		fileRows(), fileRows(file(blobfs.StatusPending, 1)), // created
		fileRows(), fileRows(file(blobfs.StatusAvailable, 2)), // the completion matches no pending row at 1
		fileRows(file(blobfs.StatusAvailable, 2)), // the row read back
	)
	got, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st))
	if err != nil || stored || got.Status != blobfs.StatusAvailable || got.Version != 2 {
		t.Fatalf("Ensure = %+v, %v, %v; want the other writer's row, found", got, stored, err)
	}
	sameOps(t, rec, begin, q, q, commit, q, q, q)
}

// A concurrent writer that commits the row between the lookup and the
// insert aborts this one's transaction; begin runs once more in a fresh
// transaction, which finds the other's pending row and resumes it.
func TestEnsure_ALostInsertRaceRetriesInAFreshTransaction(t *testing.T) {
	ctx := context.Background()
	taken := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: blobfs.ConstraintPrimaryKeyFile, Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate key")}}
	st, db, rec, _ := protocols(t,
		fileRows(), taken, // the lookup finds nothing, the insert loses
		fileRows(file(blobfs.StatusPending, 1)),   // the retry finds the other's pending row
		fileRows(file(blobfs.StatusAvailable, 2)), // and completes it
	)
	if _, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st)); err != nil || !stored {
		t.Fatalf("Ensure = %v, %v; want the resumed row stored", stored, err)
	}
	sameOps(t, rec, begin, q, q, rollback, begin, q, commit, q)
}

// A put that fails abandons the created row as Write abandons it, so the
// name and the id are free for the next run, but only at the pending
// version it holds: a row another writer resumed and completed meanwhile
// is not retired.
func TestEnsure_AFailedPutAbandonsTheRow(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t,
		fileRows(), fileRows(file(blobfs.StatusPending, 1)),
		fileRows(file(blobfs.StatusDeleting, 2)), exec(1),
	)
	failed := errors.New("put failed")
	fake.FailPut(failed)
	if _, stored, err := st.Ensure(ctx, db, strings.NewReader("report"), 6, ensure(ctx, st)); !errors.Is(err, failed) || stored {
		t.Fatalf("Ensure = %v, %v; want the put's error", stored, err)
	}
	sameOps(t, rec, begin, q, q, commit, begin, q, commit, x)
	if del := rec.Calls()[5]; !strings.HasPrefix(del.SQL, "UPDATE blobfs_file") || fmt.Sprint(del.Args) != fmt.Sprint([]any{fileID, int64(1)}) {
		t.Errorf("delete = %q %v; want the pending row's, at its version", del.SQL, del.Args)
	}
}

// The delete: pick in the transaction that begins blobfs's delete, under
// the version guard, then the object, then the purge on the pool.
func TestRetire_DeletesTheObjectThenPurges(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t, exec(1), fileRows(file(blobfs.StatusDeleting, 3)), exec(1))
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	err := st.Retire(ctx, db, func(tx *sqlate.Tx) (string, error) {
		_, err := tx.ExecContext(ctx, "DELETE FROM owner WHERE file_id = $1", fileID)
		return fileID, err
	}, bfdata.AtVersion(2))
	if err != nil {
		t.Fatalf("Retire = %v", err)
	}
	if stored(t, fake) {
		t.Error("the object outlived its retire")
	}
	sameOps(t, rec, begin, x, q, commit, x)
	if del := rec.Calls()[2]; fmt.Sprint(del.Args) != fmt.Sprint([]any{fileID, int64(2)}) {
		t.Errorf("delete args = %v; want the file at the guarded version", del.Args)
	}
}

// A pick that fails rolls back, and nothing is deleted.
func TestRetire_ARefusedPickDeletesNothing(t *testing.T) {
	ctx := context.Background()
	st, db, rec, fake := protocols(t)
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("out of scope")
	if err := st.Retire(ctx, db, func(*sqlate.Tx) (string, error) { return "", refused }); !errors.Is(err, refused) {
		t.Fatalf("Retire = %v; want pick's error", err)
	}
	if !stored(t, fake) {
		t.Error("a refused retire deleted the object")
	}
	sameOps(t, rec, begin, rollback)
}

// Serve describes an available file and opens its object on demand; any
// other status is not found.
func TestServe(t *testing.T) {
	ctx := context.Background()
	st, _, _, fake := protocols(t)
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	f := file(blobfs.StatusAvailable, 2)
	obj, open, err := st.Serve(ctx, f)
	if err != nil || obj.ContentType != "text/plain" || obj.Size != 6 || obj.ETag != `"etag"` || !obj.ModifiedAt.Equal(f.UpdatedAt) {
		t.Fatalf("Serve = %+v, %v", obj, err)
	}
	body, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	if b, _ := io.ReadAll(body); string(b) != "report" {
		t.Errorf("body = %q", b)
	}
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusDeleting} {
		if _, open, err := st.Serve(ctx, file(status, 3)); !errors.Is(err, blobfs.ErrNotFound) || open != nil {
			t.Errorf("Serve(%s) = %v; want not found", status, err)
		}
	}
}
