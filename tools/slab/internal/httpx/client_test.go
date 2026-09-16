package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
)

// echo answers every request with a JSON record of what it received.
type echo struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Custom      string `json:"custom"`
	Body        string `json:"body"`
}

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-1")
		_ = json.NewEncoder(w).Encode(echo{
			Method:      r.Method,
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Custom:      r.Header.Get("X-Custom"),
			Body:        string(body),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientDo(t *testing.T) {
	srv := echoServer(t)
	// A trailing slash on the base must not double up against the path.
	c := httpx.NewClient(srv.URL + "/")
	ctx := context.Background()

	type payload struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name     string
		call     func() (*httpx.Response, error)
		method   string
		body     string
		wantType string
	}{
		{
			name:   "get sends no body and no content type",
			call:   func() (*httpx.Response, error) { return c.Get(ctx, "/things") },
			method: http.MethodGet,
		},
		{
			name:     "post encodes a value as JSON",
			call:     func() (*httpx.Response, error) { return c.Post(ctx, "/things", payload{Name: "x"}) },
			method:   http.MethodPost,
			body:     `{"name":"x"}`,
			wantType: "application/json",
		},
		{
			name:     "put sends a string body as is",
			call:     func() (*httpx.Response, error) { return c.Put(ctx, "/things", "raw text") },
			method:   http.MethodPut,
			body:     "raw text",
			wantType: "application/json",
		},
		{
			name:     "do sends a byte body as is",
			call:     func() (*httpx.Response, error) { return c.Do(ctx, http.MethodPatch, "/things", []byte(`[1]`)) },
			method:   http.MethodPatch,
			body:     `[1]`,
			wantType: "application/json",
		},
		{
			name:   "delete sends no body",
			call:   func() (*httpx.Response, error) { return c.Delete(ctx, "/things/1") },
			method: http.MethodDelete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.call()
			if err != nil {
				t.Fatal(err)
			}
			if err := res.Expect(http.StatusOK); err != nil {
				t.Fatal(err)
			}
			if got := res.Header.Get("X-Request-Id"); got != "req-1" {
				t.Errorf("X-Request-Id = %q, want req-1", got)
			}
			var got echo
			if err := res.JSON(&got); err != nil {
				t.Fatal(err)
			}
			if got.Method != tt.method {
				t.Errorf("method = %q, want %q", got.Method, tt.method)
			}
			if !strings.HasPrefix(got.Path, "/things") {
				t.Errorf("path = %q, want /things...", got.Path)
			}
			if got.Body != tt.body {
				t.Errorf("body = %q, want %q", got.Body, tt.body)
			}
			if got.ContentType != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got.ContentType, tt.wantType)
			}
		})
	}
}

func TestClientHeaders(t *testing.T) {
	srv := echoServer(t)
	c := httpx.NewClient(srv.URL)
	res, err := c.Get(context.Background(), "/", httpx.Header{Name: "X-Custom", Value: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	var got echo
	if err := res.JSON(&got); err != nil {
		t.Fatal(err)
	}
	if got.Custom != "yes" {
		t.Errorf("X-Custom = %q, want yes", got.Custom)
	}
}

func TestClientEncodeError(t *testing.T) {
	c := httpx.NewClient("http://127.0.0.1:0")
	_, err := c.Post(context.Background(), "/", func() {})
	if err == nil || !strings.Contains(err.Error(), "encode body") {
		t.Fatalf("err = %v, want an encode error", err)
	}
}

func TestResponseExpect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such thing", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := httpx.NewClient(srv.URL)
	res, err := c.Get(context.Background(), "/missing")
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Expect(http.StatusNotFound); err != nil {
		t.Errorf("Expect(404) = %v, want nil", err)
	}
	err = res.Expect(http.StatusOK)
	if err == nil {
		t.Fatal("Expect(200) = nil, want an error")
	}
	for _, want := range []string{"404 Not Found", "want 200", "no such thing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expect(200) = %q, want it to contain %q", err, want)
		}
	}
}

func TestResponseJSONError(t *testing.T) {
	res := &httpx.Response{Status: http.StatusOK, Body: []byte("not json")}
	var v map[string]any
	err := res.JSON(&v)
	if err == nil || !strings.Contains(err.Error(), "not json") {
		t.Fatalf("JSON = %v, want a decode error quoting the body", err)
	}
}

func TestClientContextCancel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	c := httpx.NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := c.Get(ctx, "/slow")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestClientContextDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	c := httpx.NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, "/slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
