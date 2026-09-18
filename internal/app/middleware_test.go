package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// get serves one GET at path from remoteAddr through handler and returns
// the recorder.
func get(handler http.Handler, remoteAddr, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	handler.ServeHTTP(rec, req)
	return rec
}

// A request over the configured limit is answered with a 429; the health
// and ready probes are exempt regardless of how far the limit is exceeded.
func TestMiddleware_RateLimit(t *testing.T) {
	cfg := configtest.Config(t)
	requests := 2
	cfg.RateLimit.Requests = &requests

	infra := &Infrastructure{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := web.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), middleware(infra, cfg)...)

	for i := 1; i <= 2; i++ {
		if rec := get(handler, "192.0.2.1:1234", "/orders"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i, rec.Code)
		}
	}
	if rec := get(handler, "192.0.2.1:1234", "/orders"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	for i := 1; i <= 5; i++ {
		if rec := get(handler, "192.0.2.1:1234", web.HealthPath); rec.Code != http.StatusNoContent {
			t.Errorf("probe request %d: status = %d, want 204 (never rate-limited)", i, rec.Code)
		}
	}
}
