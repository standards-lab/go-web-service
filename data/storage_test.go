package data_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
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

// objects starts a store over the fake, as the composition root starts the
// real one, and returns the adapter the domains use.
func objects(t *testing.T, fake *storagetest.Fake) *data.Objects {
	t.Helper()
	cfg := storage.Config{Container: "test"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	store := storage.New(fake, cfg)
	if err := store.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Shutdown(context.Background()) })
	return data.NewStorage(nil, store).Objects
}

func TestObjects_PutEchoesTheDeclaredType(t *testing.T) {
	o := objects(t, storagetest.NewFake())

	obj, err := o.PutObject(context.Background(), "1/logo.png", strings.NewReader("png"), "image/png", 3)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ContentType != "image/png" || obj.Size != 3 || obj.ETag == "" {
		t.Errorf("object = %+v, want the declared type, the size, and an etag", obj)
	}
	body, err := o.Open(context.Background(), "1/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	if b, _ := io.ReadAll(body); string(b) != "png" {
		t.Errorf("body = %q", b)
	}
}

// The delete is idempotent: an object deleted twice, or never stored, is
// success, as blobfs's ObjectDeleter requires.
func TestObjects_DeleteObjectIsIdempotent(t *testing.T) {
	o := objects(t, storagetest.NewFake())
	ctx := context.Background()

	if _, err := o.PutObject(ctx, "1/q3.txt", strings.NewReader("q3"), "text/plain", 2); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := o.DeleteObject(ctx, "1/q3.txt"); err != nil {
			t.Errorf("DeleteObject #%d = %v, want success", i+1, err)
		}
	}
	if _, err := o.Open(ctx, "1/q3.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Open after DeleteObject = %v, want ErrNotFound", err)
	}
	if err := o.DeleteObject(ctx, "1/never-stored"); err != nil {
		t.Errorf("DeleteObject of a missing object = %v, want success", err)
	}
}

// A missing container refuses every operation as the store's fault:
// storage.ErrContainerNotFound, never an absent object, so a delete is not
// read as done, and Status answers each 503.
func TestObjects_AMissingContainerIsRefused(t *testing.T) {
	fake := storagetest.NewFake()
	o := objects(t, fake)
	ctx := context.Background()
	if _, err := o.PutObject(ctx, "1/logo.png", strings.NewReader("png"), "image/png", 3); err != nil {
		t.Fatal(err)
	}
	fake.DropContainer()

	_, putErr := o.PutObject(ctx, "1/logo.png", strings.NewReader("png"), "image/png", 3)
	body, openErr := o.Open(ctx, "1/logo.png")
	if body != nil {
		_ = body.Close()
	}
	for op, err := range map[string]error{
		"PutObject":    putErr,
		"Open":         openErr,
		"DeleteObject": o.DeleteObject(ctx, "1/logo.png"),
	} {
		if !errors.Is(err, storage.ErrContainerNotFound) || errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s = %v, want storage.ErrContainerNotFound alone", op, err)
		}
		if p, ok := data.Status(err); !ok || p.Status != http.StatusServiceUnavailable {
			t.Errorf("Status(%s) = %+v, %t; want 503", op, p, ok)
		}
	}
}

// A put that fails because the upload's body did is ErrBodyRead, a 400,
// never the 503 a lost database connection's io.ErrUnexpectedEOF is.
func TestObjects_ABodyCutShortIsTheRequests(t *testing.T) {
	o := objects(t, storagetest.NewFake())

	body := io.MultiReader(strings.NewReader("pn"), iotest.ErrReader(io.ErrUnexpectedEOF))
	_, err := o.PutObject(context.Background(), "1/logo.png", body, "image/png", 3)
	if !errors.Is(err, data.ErrBodyRead) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("PutObject = %v, want ErrBodyRead wrapping the read's error", err)
	}
	if p, ok := data.Status(err); !ok || p.Status != http.StatusBadRequest {
		t.Errorf("Status = %+v, %t; want 400", p, ok)
	}
}

func TestObjects_ValidateKeyIsTheStoresRule(t *testing.T) {
	o := objects(t, storagetest.NewFake())

	if err := o.ValidateKey("1/logo.png"); err != nil {
		t.Errorf("ValidateKey(valid) = %v", err)
	}
	if err := o.ValidateKey(""); err == nil {
		t.Error("ValidateKey(empty) = nil, want the store's refusal")
	}
}

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
	dl, err := st.Serve(ctx, f)
	obj := dl.Object
	if err != nil || obj.ContentType != "text/plain" || obj.Size != 6 || obj.ETag != `"etag"` || !obj.ModifiedAt.Equal(f.UpdatedAt) {
		t.Fatalf("Serve = %+v, %v", obj, err)
	}
	body, err := dl.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	if b, _ := io.ReadAll(body); string(b) != "report" {
		t.Errorf("body = %q", b)
	}
	for _, status := range []blobfs.Status{blobfs.StatusPending, blobfs.StatusDeleting} {
		if dl, err := st.Serve(ctx, file(status, 3)); !errors.Is(err, blobfs.ErrNotFound) || dl.Open != nil {
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
	dl, err := st.Serve(ctx, file(blobfs.StatusAvailable, 2))
	if err != nil {
		t.Fatalf("Serve = %v; the description needs no object", err)
	}
	body, err := dl.Open()
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
