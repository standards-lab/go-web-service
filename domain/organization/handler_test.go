package organization_test

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/organization"
)

// module compiles the layer's route group the way the composition root
// does, over the scripted driver with the given responses; with none
// scripted, only the handler-local rejection paths can answer.
func module(t *testing.T, responses ...sqltest.Response) http.Handler {
	t.Helper()
	svc, _ := service(t, responses...)
	r := web.NewRouter()
	r.Mount(web.NewModule(organization.Routes(svc, web.Limits{DefaultSize: 20, MaxSize: 100})))
	return r
}

func send(t *testing.T, h http.Handler, method, path, ifMatch, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
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

func TestRoutes_RejectBeforeAnyOperation(t *testing.T) {
	cases := map[string]struct {
		method, path, ifMatch, body string
		status                      int
		detail                      string
	}{
		"list: malformed page":       {"GET", "/organizations?page=x", "", "", 400, "query page"},
		"list: oversized page":       {"GET", "/organizations?size=1000", "", "", 400, "query size"},
		"find: malformed id":         {"GET", "/organizations/not-a-uuid", "", "", 400, "must be a UUID"},
		"create: malformed body":     {"POST", "/organizations", "", "{", 400, "body:"},
		"create: unknown field":      {"POST", "/organizations", "", `{"codex":"a"}`, 400, "codex"},
		"create: empty body":         {"POST", "/organizations", "", " ", 400, "empty body"},
		"create: bad code":           {"POST", "/organizations", "", `{"code":"Bad_Code","name":"X"}`, 400, "code must be"},
		"create: empty name":         {"POST", "/organizations", "", `{"code":"ok","name":""}`, 400, "name must not be empty"},
		"create: bad parent":         {"POST", "/organizations", "", `{"parent_id":"nope","code":"ok","name":"X"}`, 400, "parent_id must be a UUID"},
		"edit: missing If-Match":     {"PUT", "/organizations/" + validID, "", `{"code":"ok","name":"X"}`, 428, "If-Match"},
		"transfer: missing If-Match": {"POST", "/organizations/" + validID + "/transfer", "", `{"parent_id":null}`, 428, "If-Match"},
		"delete: missing If-Match":   {"DELETE", "/organizations/" + validID, "", "", 428, "If-Match"},
		"edit: malformed If-Match":   {"PUT", "/organizations/" + validID, `W/"3"`, `{"code":"ok","name":"X"}`, 400, "If-Match"},
		"edit: malformed id":         {"PUT", "/organizations/not-a-uuid", `"1"`, `{"code":"ok","name":"X"}`, 400, "must be a UUID"},
		"edit: missing name":         {"PUT", "/organizations/" + validID, `"1"`, `{"code":"ok"}`, 400, "name must not be empty"},
		"transfer: bad parent":       {"POST", "/organizations/" + validID + "/transfer", `"1"`, `{"parent_id":"nope"}`, 400, "parent_id must be a UUID"},
		"transfer: key omitted":      {"POST", "/organizations/" + validID + "/transfer", `"1"`, `{}`, 400, "parent_id is required"},
		"edit on PATCH is unrouted":  {"PATCH", "/organizations/" + validID, `"1"`, `{"code":"ok","name":"X"}`, 405, ""},
	}
	h := module(t)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, h, c.method, c.path, c.ifMatch, c.body)
			if c.detail == "" {
				if rec.Code != c.status {
					t.Fatalf("status = %d, want %d", rec.Code, c.status)
				}
				return
			}
			body := problem(t, rec, c.status)
			if detail, _ := body["detail"].(string); !strings.Contains(detail, c.detail) {
				t.Errorf("detail = %q, want it to contain %q", detail, c.detail)
			}
		})
	}
}

func TestRoutes_OversizedBodyIs413(t *testing.T) {
	rec := send(t, module(t), "POST", "/organizations", "", `{"name":"`+strings.Repeat("x", 1<<16)+`"}`)
	problem(t, rec, 413)
}

func TestRoutes_LibraryErrorsMapThroughTheDataMatcher(t *testing.T) {
	cases := map[string]struct {
		responses             []sqltest.Response
		method, path, ifMatch string
		status                int
	}{
		"unknown filter field is 400": {nil, "GET", "/organizations?nope=1", "", 400},
		"unknown operator is 400":     {nil, "GET", "/organizations?code[between]=a", "", 400},
		"absent row is 404": {
			[]sqltest.Response{{Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"}}},
			"GET", "/organizations/" + validID, "", 404,
		},
		"stale version is 412": {
			[]sqltest.Response{{Affected: 0}, {Columns: []string{"version"}, Rows: [][]driver.Value{{int64(9)}}}},
			"DELETE", "/organizations/" + validID, `"1"`, 412,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, module(t, c.responses...), c.method, c.path, c.ifMatch, "")
			problem(t, rec, c.status)
		})
	}
}

func TestCreate_AnswersCreatedWithLocation(t *testing.T) {
	rec := send(t, module(t, identity(validID, 1)), "POST", "/organizations", "", `{"code":"acme","name":"Acme"}`)
	if rec.Code != 201 || rec.Header().Get("Location") != "/organizations/"+validID {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
}
