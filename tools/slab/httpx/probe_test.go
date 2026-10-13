package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

func TestProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("200 on the path answers nil", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		if err := httpx.Probe(ctx, srv.URL+"/", web.ReadyPath); err != nil {
			t.Fatalf("Probe = %v, want nil", err)
		}
		if gotPath != web.ReadyPath {
			t.Errorf("probed %q, want %q", gotPath, web.ReadyPath)
		}
	})

	t.Run("another status is a StatusError naming it", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		err := httpx.Probe(ctx, srv.URL, web.ReadyPath)
		var status *httpx.StatusError
		if !errors.As(err, &status) || status.Status != http.StatusServiceUnavailable || status.Path != web.ReadyPath {
			t.Fatalf("Probe = %v, want a StatusError of 503 on %s", err, web.ReadyPath)
		}
		if want := "GET /readyz: status 503 Service Unavailable"; err.Error() != want {
			t.Errorf("Probe = %q, want %q", err, want)
		}
	})

	t.Run("nothing listening is the transport's error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		err := httpx.Probe(ctx, srv.URL, web.ReadyPath)
		var status *httpx.StatusError
		if err == nil || errors.As(err, &status) {
			t.Fatalf("Probe = %v, want a transport error", err)
		}
	})
}

func TestProbeGrafana_ProbesTheHealthEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := httpx.ProbeGrafana(context.Background(), srv.URL); err != nil {
		t.Fatalf("ProbeGrafana = %v, want nil", err)
	}
	if gotPath != "/api/health" {
		t.Errorf("probed %q, want /api/health", gotPath)
	}
}
