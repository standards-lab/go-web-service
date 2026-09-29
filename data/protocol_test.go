package data_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

const q, x = sqltest.OpQuery, sqltest.OpExec

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

// file is a file row at a status and version; an available one carries
// the size and entity tag its completion recorded.
func file(status blobfs.Status, version int64) blobfs.File {
	now := time.Now()
	f := blobfs.File{ID: fileID, DirectoryID: dirID, Name: "report.txt", Status: status, Key: key, ContentType: "text/plain", Version: version, CreatedAt: now, UpdatedAt: now}
	if status != blobfs.StatusPending {
		size, etag := int64(6), `"etag"`
		f.Size, f.ETag = &size, &etag
	}
	return f
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

// A download whose container is missing is an outage, not an absent file:
// go-storage reports the open as storage.ErrContainerNotFound, never
// storage.ErrNotFound, and Status answers it 503.
func TestServe_AMissingContainerIsAnOutage(t *testing.T) {
	ctx := context.Background()
	st, _, _, fake := protocols(t)
	if _, err := fake.Put(ctx, key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	fake.DropContainer()
	_, open, err := st.Serve(ctx, file(blobfs.StatusAvailable, 2))
	if err != nil {
		t.Fatalf("Serve = %v; the description needs no object", err)
	}
	body, err := open()
	if !errors.Is(err, storage.ErrContainerNotFound) || errors.Is(err, storage.ErrNotFound) {
		if body != nil {
			_ = body.Close()
		}
		t.Fatalf("open = %v; want storage.ErrContainerNotFound alone", err)
	}
	if p, ok := data.Status(err); !ok || p.Status != http.StatusServiceUnavailable {
		t.Errorf("Status(open) = %+v, %t; want 503", p, ok)
	}
}
