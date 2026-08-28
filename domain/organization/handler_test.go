package organization_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/domain/organization"
)

// validID is any well-formed UUID: these tests exercise the rejection paths
// that answer before the row would matter.
const validID = "00000000-0000-7000-8000-000000000000"

// module compiles the layer's route group the way the composition root
// does. The nil database is never reached: these are the handler-local
// rejection paths, which answer before any operation runs — the
// database-backed paths are proven against the compose stack.
func module(t *testing.T) *web.Module {
	t.Helper()
	svc := organization.New(nil)
	return web.NewModule(organization.Routes(svc, web.Limits{DefaultSize: 20, MaxSize: 100}))
}

// send drives one request through the module: a command's If-Match header
// and body attach when given.
func send(t *testing.T, method, path, ifMatch, body string) *httptest.ResponseRecorder {
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
	module(t).ServeHTTP(rec, r)
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

func TestList_RejectsMalformedDirectives(t *testing.T) {
	rec := send(t, "GET", "/organizations?page=x", "", "")

	body := problem(t, rec, 400)
	if body["detail"] == nil {
		t.Error("400 problem carries no detail")
	}
}

func TestList_RejectsOversizedPage(t *testing.T) {
	rec := send(t, "GET", "/organizations?size=1000", "", "")

	problem(t, rec, 400)
}

func TestFind_RejectsMalformedID(t *testing.T) {
	rec := send(t, "GET", "/organizations/not-a-uuid", "", "")

	body := problem(t, rec, 400)
	if body["detail"] != "invalid command: id must be a UUID" {
		t.Errorf("detail = %v, want the UUID message", body["detail"])
	}
}

func TestCreate_RejectsMalformedBody(t *testing.T) {
	rec := send(t, "POST", "/organizations", "", "{")

	body := problem(t, rec, 400)
	if body["detail"] == nil {
		t.Error("400 problem carries no detail")
	}
}

func TestCreate_RejectsUnknownField(t *testing.T) {
	rec := send(t, "POST", "/organizations", "", `{"codex":"a"}`)

	problem(t, rec, 400)
}

func TestCreate_RejectsBadCode(t *testing.T) {
	rec := send(t, "POST", "/organizations", "", `{"code":"Bad_Code","name":"X"}`)

	body := problem(t, rec, 400)
	detail, _ := body["detail"].(string)
	if !strings.Contains(detail, "code") {
		t.Errorf("detail = %q, want it to name the code rule", detail)
	}
}

func TestCreate_RejectsEmptyName(t *testing.T) {
	rec := send(t, "POST", "/organizations", "", `{"code":"ok","name":""}`)

	problem(t, rec, 400)
}

func TestCreate_RejectsMalformedParentID(t *testing.T) {
	rec := send(t, "POST", "/organizations", "", `{"parent_id":"nope","code":"ok","name":"X"}`)

	problem(t, rec, 400)
}

func TestCommands_RequireIfMatch(t *testing.T) {
	cases := map[string]struct {
		method, path, body string
	}{
		"edit":     {"PATCH", "/organizations/" + validID, `{"code":"ok","name":"X"}`},
		"transfer": {"POST", "/organizations/" + validID + "/transfer", `{}`},
		"delete":   {"DELETE", "/organizations/" + validID, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, c.method, c.path, "", c.body)

			body := problem(t, rec, 428)
			if detail, ok := body["detail"].(string); ok && detail != "" {
				t.Errorf("428 problem carries detail %q; only a 400 carries detail", detail)
			}
		})
	}
}

func TestEdit_RejectsMalformedIfMatch(t *testing.T) {
	for _, header := range []string{`3`, `*`, `W/"3"`, `"abc"`, `"1", "2"`} {
		t.Run(header, func(t *testing.T) {
			rec := send(t, "PATCH", "/organizations/"+validID, header, `{"code":"ok","name":"X"}`)

			problem(t, rec, 400)
		})
	}
}

func TestEdit_RejectsMalformedID(t *testing.T) {
	rec := send(t, "PATCH", "/organizations/not-a-uuid", `"1"`, `{"code":"ok","name":"X"}`)

	problem(t, rec, 400)
}

func TestTransfer_RejectsMalformedParentID(t *testing.T) {
	rec := send(t, "POST", "/organizations/"+validID+"/transfer", `"1"`, `{"parent_id":"nope"}`)

	problem(t, rec, 400)
}
