package httpx_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

func TestResponseTraceID(t *testing.T) {
	for name, tc := range map[string]struct {
		id string
		ok bool
	}{
		"32 lowercase hex digits": {"4bf92f3577b34da6a3ce929d0e0e4736", true},
		"no header":               {"", false},
		"31 digits":               {"4bf92f3577b34da6a3ce929d0e0e473", false},
		"33 digits":               {"4bf92f3577b34da6a3ce929d0e0e47366", false},
		"uppercase":               {"4BF92F3577B34DA6A3CE929D0E0E4736", false},
		"not hex":                 {"4bf92f3577b34da6a3ce929d0e0e473g", false},
		"a generated request id":  {"req-4bf92f3577b34da6a3ce929d0e0e", false},
	} {
		t.Run(name, func(t *testing.T) {
			res := &httpx.Response{Status: http.StatusOK, Header: http.Header{}}
			if tc.id != "" {
				res.Header.Set("X-Request-Id", tc.id)
			}
			got, err := res.TraceID()
			if (err == nil) != tc.ok {
				t.Fatalf("TraceID with X-Request-Id %q = %q, %v; want ok=%v", tc.id, got, err, tc.ok)
			}
			if tc.ok && got != tc.id {
				t.Errorf("TraceID = %q, want %q", got, tc.id)
			}
		})
	}
}

func TestResponseTraceID_NamesTheMissingHeader(t *testing.T) {
	res := &httpx.Response{Status: http.StatusOK, Header: http.Header{}}
	_, err := res.TraceID()
	if err == nil || !strings.Contains(err.Error(), "X-Request-Id") {
		t.Fatalf("TraceID = %v, want an error naming the header", err)
	}
}

func problemResponse(status int, contentType, body string) *httpx.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &httpx.Response{Status: status, Header: h, Body: []byte(body)}
}

func TestResponseProblem(t *testing.T) {
	const body = `{"type":"about:blank","title":"Not Found","status":404,"detail":"no such row","instance":"/api/organizations/x","request_id":"4bf92f3577b34da6a3ce929d0e0e4736"}`

	t.Run("decodes a problem document at its status", func(t *testing.T) {
		p, err := problemResponse(http.StatusNotFound, web.ProblemMediaType, body).Problem(http.StatusNotFound)
		if err != nil {
			t.Fatalf("Problem = %v, want nil", err)
		}
		if p.Status != http.StatusNotFound || p.Title != "Not Found" || p.Detail != "no such row" || p.Instance != "/api/organizations/x" {
			t.Errorf("Problem = %+v", p)
		}
		if got := p.Extras["request_id"]; got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("request_id = %v, want the trace id", got)
		}
	})

	t.Run("accepts a media type with parameters", func(t *testing.T) {
		if _, err := problemResponse(http.StatusNotFound, web.ProblemMediaType+"; charset=utf-8", body).Problem(http.StatusNotFound); err != nil {
			t.Fatalf("Problem = %v, want nil", err)
		}
	})

	t.Run("rejects the wrong status", func(t *testing.T) {
		_, err := problemResponse(http.StatusNotFound, web.ProblemMediaType, body).Problem(http.StatusBadRequest)
		if err == nil || !strings.Contains(err.Error(), "want 400") {
			t.Fatalf("Problem = %v, want a status error", err)
		}
	})

	t.Run("rejects a body that is not a problem document", func(t *testing.T) {
		_, err := problemResponse(http.StatusNotFound, "text/plain; charset=utf-8", "404 page not found").Problem(http.StatusNotFound)
		if err == nil || !strings.Contains(err.Error(), web.ProblemMediaType) {
			t.Fatalf("Problem = %v, want a Content-Type error naming the problem media type", err)
		}
	})

	t.Run("rejects a missing content type", func(t *testing.T) {
		if _, err := problemResponse(http.StatusNotFound, "", body).Problem(http.StatusNotFound); err == nil {
			t.Fatal("Problem = nil, want a Content-Type error")
		}
	})

	t.Run("rejects a document whose status disagrees with the response", func(t *testing.T) {
		_, err := problemResponse(http.StatusNotFound, web.ProblemMediaType, `{"status":400,"title":"Bad Request"}`).Problem(http.StatusNotFound)
		if err == nil || !strings.Contains(err.Error(), "says status 400") {
			t.Fatalf("Problem = %v, want a disagreement error", err)
		}
	})

	t.Run("rejects a body that does not decode", func(t *testing.T) {
		if _, err := problemResponse(http.StatusNotFound, web.ProblemMediaType, "not json").Problem(http.StatusNotFound); err == nil {
			t.Fatal("Problem = nil, want a decode error")
		}
	})
}
