package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func TestTree_Get(t *testing.T) {
	tree := Tree{"acme": {ID: "x", Code: "acme"}}
	o, err := tree.Get("acme")
	if err != nil || o.ID != "x" {
		t.Errorf("Get(acme) = %+v, %v", o, err)
	}
	if _, err := tree.Get("sales"); err == nil || !strings.Contains(err.Error(), `"sales"`) {
		t.Errorf("Get(sales) = %v, want an error naming the code", err)
	}
}

func TestIdentityOf(t *testing.T) {
	res := &httpx.Response{Status: http.StatusOK, Body: []byte(`{"id":"x","version":2}`)}
	id, err := IdentityOf(res, "x")
	if err != nil || id != (organization.Identity{ID: "x", Version: 2}) {
		t.Errorf("IdentityOf = %+v, %v", id, err)
	}
	if _, err := IdentityOf(res, "y"); err == nil {
		t.Error("IdentityOf with the wrong id = nil, want an error")
	}
}

func TestExpectVersion(t *testing.T) {
	if err := ExpectVersion(organization.Identity{Version: 2}, 2); err != nil {
		t.Errorf("ExpectVersion(2, 2) = %v", err)
	}
	if err := ExpectVersion(organization.Identity{Version: 1}, 2); err == nil {
		t.Error("ExpectVersion(1, 2) = nil, want an error")
	}
}

func TestOversizedBody_IsValidJSONPastTheLimit(t *testing.T) {
	payload, printable := OversizedBody()
	if len(payload) <= maxCommandBody {
		t.Errorf("payload is %d bytes, want more than %d", len(payload), maxCommandBody)
	}
	var body organization.CreateOrganization
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	if len(body.Code) != maxCommandBody+1 {
		t.Errorf("code is %d bytes, want %d", len(body.Code), maxCommandBody+1)
	}
	if want := `{"code":"<65537 bytes>"}`; printable != want {
		t.Errorf("printable = %q, want %q", printable, want)
	}
}

// listServer answers the raw list with one seeded row and records the
// request line.
func listServer(t *testing.T, status int) (*httptest.Server, *[]string) {
	t.Helper()
	var lines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines = append(lines, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", web.JSONMediaType)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(organization.Page{Items: []organization.Organization{{ID: "x", Code: "acme", Path: "/acme"}}, Page: 1, Size: 20, Total: 1})
	}))
	t.Cleanup(srv.Close)
	return srv, &lines
}

func TestList_NarratesTheRawListAndDecodesIt(t *testing.T) {
	srv, lines := listServer(t, http.StatusOK)
	var out bytes.Buffer
	p, err := List(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false))
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Code != "acme" {
		t.Errorf("List = %+v", p)
	}
	if want := []string{"GET " + organization.Organizations}; strings.Join(*lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests = %q, want %q", *lines, want)
	}
	for _, want := range []string{"GET /api/organizations\n", "HTTP 200 OK", `"code": "acme"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("narration lacks %q:\n%s", want, out.String())
		}
	}
}

func TestList_FailsOnAnotherStatus(t *testing.T) {
	srv, _ := listServer(t, http.StatusServiceUnavailable)
	var out bytes.Buffer
	_, err := List(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false))
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("List = %v, want a 503 error", err)
	}
}

func TestReset_ShowsTheSeedFileThenPostsTheState(t *testing.T) {
	var got struct {
		line string
		body map[string]string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.line = r.Method + " " + r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", web.JSONMediaType)
		_ = json.NewEncoder(w).Encode(map[string]string{"state": SeedState})
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	if err := Reset(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false)); err != nil {
		t.Fatalf("Reset = %v\n%s", err, out.String())
	}
	if got.line != "POST /admin/database/state" || got.body["state"] != SeedState {
		t.Errorf("the service received %s %v", got.line, got.body)
	}
	caption, reset := strings.Index(out.String(), "  data/seeds/default.json\n"), strings.Index(out.String(), "POST /admin/database/state")
	if caption < 0 || reset < 0 || caption > reset {
		t.Fatalf("the seed file does not precede the reset request:\n%s", out.String())
	}
	if !strings.Contains(out.String()[caption:reset], `"code": "acme"`) {
		t.Errorf("the seed block lacks the acme row:\n%s", out.String()[caption:reset])
	}
}

func TestReset_FailsWhenTheRepoIsNotARoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	ctx := env.WithContext(context.Background(), env.Env{Repo: t.TempDir()})
	var out bytes.Buffer
	if err := Reset(ctx, httpx.NewClient(srv.URL), scenario.NewReporter(&out, false)); err == nil {
		t.Fatal("Reset = nil, want the repo error")
	}
}
