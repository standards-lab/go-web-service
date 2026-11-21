package data_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{}, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := sqltest.Open(t)
	return data.NewStorage(data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog), fs, store).Objects
}

func TestObjects_PutEchoesTheDeclaredType(t *testing.T) {
	o := objects(t, storagetest.NewFake(storagetest.Options{}))

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

func TestObjects_DeleteObjectIsIdempotent(t *testing.T) {
	o := objects(t, storagetest.NewFake(storagetest.Options{}))
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

func TestObjects_AMissingContainerIsRefused(t *testing.T) {
	fake := storagetest.NewFake(storagetest.Options{})
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

func TestObjects_ABodyCutShortIsTheRequests(t *testing.T) {
	o := objects(t, storagetest.NewFake(storagetest.Options{}))

	body := io.MultiReader(strings.NewReader("pn"), iotest.ErrReader(io.ErrUnexpectedEOF))
	_, err := o.PutObject(context.Background(), "1/logo.png", body, "image/png", 3)
	if !errors.Is(err, data.ErrBodyRead) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("PutObject = %v, want ErrBodyRead wrapping the read's error", err)
	}
	if p, ok := data.Status(err); !ok || p.Status != http.StatusBadRequest {
		t.Errorf("Status = %+v, %t; want 400", p, ok)
	}
}

func TestObjects_ABodyReadTimeoutIsTheClients(t *testing.T) {
	for name, deadline := range map[string]error{
		"os deadline":      os.ErrDeadlineExceeded,
		"net timeout":      &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
		"context deadline": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			fake := storagetest.NewFake(storagetest.Options{})
			o := objects(t, fake)
			fake.SetDown(true)

			body := io.MultiReader(strings.NewReader("pn"), iotest.ErrReader(deadline))
			_, err := o.PutObject(context.Background(), "1/logo.png", body, "image/png", 3)
			if !errors.Is(err, data.ErrBodyTimeout) || errors.Is(err, data.ErrBodyRead) {
				t.Fatalf("PutObject = %v, want ErrBodyTimeout", err)
			}
			if p, ok := data.Status(err); !ok || p.Status != http.StatusRequestTimeout {
				t.Errorf("Status = %+v, %t; want 408", p, ok)
			}
		})
	}
}

func TestObjects_ABodyCutShortOutranksTheStoresError(t *testing.T) {
	fake := storagetest.NewFake(storagetest.Options{})
	o := objects(t, fake)
	fake.SetDown(true)

	body := io.MultiReader(strings.NewReader("pn"), iotest.ErrReader(io.ErrUnexpectedEOF))
	_, err := o.PutObject(context.Background(), "1/logo.png", body, "image/png", 3)
	if p, ok := data.Status(err); !errors.Is(err, data.ErrBodyRead) || !ok || p.Status != http.StatusBadRequest {
		t.Errorf("PutObject = %v, Status = %+v; want ErrBodyRead, 400", err, p)
	}
}

func TestObjects_ValidateKeyIsTheStoresRule(t *testing.T) {
	o := objects(t, storagetest.NewFake(storagetest.Options{}))

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

func protocols(t *testing.T, responses ...sqltest.Response) (*data.Storage, *sqlate.DB, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	dialect := sqltest.ReturningDialect{}
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, dialect, bfdata.Options{})
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
	db := sqlate.Wrap(pool, dialect)
	return data.NewStorage(data.New(db, catalog), fs, objects), db, rec, fake
}

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

func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ops = %v\nwant %v", got, want)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
}

func TestServe_AnAvailableFileOpensItsObjectAndOthersAreNotFound(t *testing.T) {
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
