package organization_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/domain/organization"
)

// module compiles the layer's route group the way the composition root
// does. The nil database is never reached: these are the handler-local
// rejection paths, which answer before any operation runs — the
// database-backed paths are proven against the compose stack.
func module(t *testing.T) *web.Module {
	t.Helper()
	svc := organization.New(nil)
	return web.NewModule(organization.Routes(svc, web.Limits{DefaultSize: 20, MaxSize: 100}))
}

func problem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, web.ProblemMediaType)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

func TestList_RejectsMalformedDirectives(t *testing.T) {
	m := module(t)
	rec := httptest.NewRecorder()

	m.ServeHTTP(rec, httptest.NewRequest("GET", "/organizations?page=x", nil))

	body := problem(t, rec, 400)
	if body["detail"] == nil {
		t.Error("400 problem carries no detail")
	}
}

func TestList_RejectsOversizedPage(t *testing.T) {
	m := module(t)
	rec := httptest.NewRecorder()

	m.ServeHTTP(rec, httptest.NewRequest("GET", "/organizations?size=1000", nil))

	problem(t, rec, 400)
}

func TestFind_RejectsMalformedID(t *testing.T) {
	m := module(t)
	rec := httptest.NewRecorder()

	m.ServeHTTP(rec, httptest.NewRequest("GET", "/organizations/not-a-uuid", nil))

	body := problem(t, rec, 400)
	if body["detail"] != "id must be a UUID" {
		t.Errorf("detail = %v, want the UUID message", body["detail"])
	}
}
