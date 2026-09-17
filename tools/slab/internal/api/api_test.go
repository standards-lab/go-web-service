package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/internal/api"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func TestCreateOrganization_MarshalsUnderTheServiceFieldNames(t *testing.T) {
	parent := "00000000-0000-0000-0000-000000000001"
	raw, err := json.Marshal(api.CreateOrganization{ParentID: &parent, Code: "sales", Name: "Sales"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":"` + parent + `","code":"sales","name":"Sales"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
	raw, err = json.Marshal(api.CreateOrganization{Code: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":null,"code":"acme","name":"Acme"}`; string(raw) != want {
		t.Errorf("root body = %s, want %s", raw, want)
	}
}

func TestPage_IsTheSDKEnvelope(t *testing.T) {
	var p api.Page
	if err := json.Unmarshal([]byte(`{"items":[{"id":"x","parent_id":null,"code":"acme","name":"Acme","version":1,"path":"/acme"}],"page":1,"size":20,"total":1}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Page != 1 || p.Size != 20 || p.Total != 1 || len(p.Items) != 1 || p.Items[0].Path != "/acme" {
		t.Errorf("Page = %+v", p)
	}
}

func TestTree_Get(t *testing.T) {
	tree := api.Tree{"acme": {ID: "x", Code: "acme"}}
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
	id, err := api.IdentityOf(res, "x")
	if err != nil || id != (api.Identity{ID: "x", Version: 2}) {
		t.Errorf("IdentityOf = %+v, %v", id, err)
	}
	if _, err := api.IdentityOf(res, "y"); err == nil {
		t.Error("IdentityOf with the wrong id = nil, want an error")
	}
}

func TestExpectVersion(t *testing.T) {
	if err := api.ExpectVersion(api.Identity{Version: 2}, 2); err != nil {
		t.Errorf("ExpectVersion(2, 2) = %v", err)
	}
	if err := api.ExpectVersion(api.Identity{Version: 1}, 2); err == nil {
		t.Error("ExpectVersion(1, 2) = nil, want an error")
	}
}

func TestOversizedBody_IsValidJSONPastTheLimit(t *testing.T) {
	payload, printable := api.OversizedBody()
	if len(payload) <= api.MaxCommandBody {
		t.Errorf("payload is %d bytes, want more than %d", len(payload), api.MaxCommandBody)
	}
	var body api.CreateOrganization
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	if len(body.Code) != api.MaxCommandBody+1 {
		t.Errorf("code is %d bytes, want %d", len(body.Code), api.MaxCommandBody+1)
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
		_ = json.NewEncoder(w).Encode(api.Page{Items: []api.Organization{{ID: "x", Code: "acme", Path: "/acme"}}, Page: 1, Size: 20, Total: 1})
	}))
	t.Cleanup(srv.Close)
	return srv, &lines
}

func TestList_NarratesTheRawListAndDecodesIt(t *testing.T) {
	srv, lines := listServer(t, http.StatusOK)
	var out bytes.Buffer
	p, err := api.List(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false))
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Code != "acme" {
		t.Errorf("List = %+v", p)
	}
	if want := []string{"GET " + api.Organizations}; strings.Join(*lines, "\n") != strings.Join(want, "\n") {
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
	_, err := api.List(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false))
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
		_ = json.NewEncoder(w).Encode(map[string]string{"state": api.SeedState})
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	if err := api.Reset(context.Background(), httpx.NewClient(srv.URL), scenario.NewReporter(&out, false)); err != nil {
		t.Fatalf("Reset = %v\n%s", err, out.String())
	}
	if got.line != "POST "+api.State || got.body["state"] != api.SeedState {
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
	if err := api.Reset(ctx, httpx.NewClient(srv.URL), scenario.NewReporter(&out, false)); err == nil {
		t.Fatal("Reset = nil, want the repo error")
	}
}
