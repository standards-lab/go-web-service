package data_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"

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

func TestObjects_ValidateKeyIsTheStoresRule(t *testing.T) {
	o := objects(t, storagetest.NewFake())

	if err := o.ValidateKey("1/logo.png"); err != nil {
		t.Errorf("ValidateKey(valid) = %v", err)
	}
	if err := o.ValidateKey(""); err == nil {
		t.Error("ValidateKey(empty) = nil, want the store's refusal")
	}
}
