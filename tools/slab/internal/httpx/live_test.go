package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
)

// withBase returns a context carrying base as the env's service base URL,
// the only field Live reads.
func withBase(ctx context.Context, base string) context.Context {
	return env.WithContext(ctx, env.Env{Base: base})
}

func TestLive(t *testing.T) {
	ctx := context.Background()

	t.Run("200 on the health path is live", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		if err := httpx.Live(withBase(ctx, srv.URL)); err != nil {
			t.Fatalf("Live = %v, want nil", err)
		}
		if gotPath != web.HealthPath {
			t.Errorf("probed %q, want %q", gotPath, web.HealthPath)
		}
	})

	t.Run("any other status is not live", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		err := httpx.Live(withBase(ctx, srv.URL))
		if err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("Live = %v, want a 503 error", err)
		}
	})

	t.Run("nothing listening is not live", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		if err := httpx.Live(withBase(ctx, srv.URL)); err == nil {
			t.Fatal("Live = nil, want a transport error")
		}
	})

	t.Run("a cancelled context is not live", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		cancelled, cancel := context.WithCancel(withBase(ctx, srv.URL))
		cancel()
		if err := httpx.Live(cancelled); err == nil {
			t.Fatal("Live = nil, want a context error")
		}
	})
}
