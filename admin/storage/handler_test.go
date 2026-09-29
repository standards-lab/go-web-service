package storage_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/go-web-sdk"

	storageadmin "github.com/standards-lab/go-web-service/admin/storage"
)

// module mounts the group over a store on the fake, started as the
// composition root starts the real one.
func module(t *testing.T, fake *storagetest.Fake) http.Handler {
	t.Helper()
	cfg := storage.Config{Container: "go-web-service"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	store := storage.New(fake, cfg)
	if err := store.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Shutdown(context.Background()) })
	r := web.NewRouter()
	r.Mount(web.NewModule(storageadmin.Routes(store, slog.New(slog.DiscardHandler))))
	return r
}

func send(t *testing.T, h http.Handler, method, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return rec.Code, body
}

func TestDiagnostics_ReadsTheStore(t *testing.T) {
	h := module(t, storagetest.NewFake())

	code, d := send(t, h, "GET", "/storage/diagnostics")
	if code != 200 || d["ready"] != true || d["container"] != "go-web-service" || d["max_key_length"] != float64(storagetest.DefaultMaxKeyLength) {
		t.Errorf("diagnostics = %d %v", code, d)
	}
}

// A container removed out from under the running service reads not ready,
// and the container operation restores it.
func TestContainer_RestoresARemovedContainer(t *testing.T) {
	fake := storagetest.NewFake()
	h := module(t, fake)
	fake.DropContainer()

	if _, d := send(t, h, "GET", "/storage/diagnostics"); d["ready"] != false {
		t.Errorf("diagnostics without the container = %v, want not ready", d)
	}
	code, d := send(t, h, "POST", "/storage/container")
	if code != 200 || d["ready"] != true || !fake.HasContainer() {
		t.Errorf("container = %d %v, has container %t", code, d, fake.HasContainer())
	}
}

func TestContainer_AnUnreachableStoreIs503WithTheReason(t *testing.T) {
	fake := storagetest.NewFake()
	h := module(t, fake)
	fake.Down.Store(true)

	code, p := send(t, h, "POST", "/storage/container")
	if detail, _ := p["detail"].(string); code != 503 || !strings.Contains(detail, "unavailable") {
		t.Errorf("container while down = %d %v, want 503 with the reason", code, p)
	}
}
