package organization_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
	"github.com/standards-lab/go-web-service/tools/slab/internal/apptest"
	"github.com/standards-lab/go-web-service/tools/slab/internal/termtest"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// exchange is one request as the fake service received it.
type exchange struct {
	method, uri, ifMatch, contentType, body string
}

// service is a fake that answers every request with one canned reply and
// records what it received.
type service struct {
	status   int
	reply    string
	received []exchange
}

func newService(t *testing.T, status int, reply string) (*service, *httptest.Server) {
	t.Helper()
	s := &service{status: status, reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.received = append(s.received, exchange{
			method:      r.Method,
			uri:         r.URL.RequestURI(),
			ifMatch:     r.Header.Get("If-Match"),
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		})
		// The service answers an error status with a problem document.
		if s.status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", web.ProblemMediaType)
		} else {
			w.Header().Set("Content-Type", web.JSONMediaType)
		}
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.reply)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

// only is the one request the service received, failing when it received
// any other number.
func (s *service) only(t *testing.T) exchange {
	t.Helper()
	if len(s.received) != 1 {
		t.Fatalf("the service received %d requests, want 1: %+v", len(s.received), s.received)
	}
	return s.received[0]
}

// result is what one run of the App did.
type result struct {
	stdout, stderr string
	code           int
	built          []string // the nodes the run built, in order
}

// built is what every org command builds: the two nodes it declares and
// the lifecycle configuration the dispatcher adds.
var built = []string{"config", "client", "lifecycle config"}

// run runs a fresh App over "org" and args, with --base naming srv, and
// returns what it did.
func run(t *testing.T, srv *httptest.Server, args ...string) result {
	t.Helper()
	var stdout bytes.Buffer
	return runTo(t, &stdout, srv, args...)
}

// runTo is run with stdout supplied by the caller, for a test that needs it
// to be a terminal; result.stdout is then empty.
func runTo(t *testing.T, stdout io.Writer, srv *httptest.Server, args ...string) result {
	t.Helper()
	var stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &stderr})
	rec := apptest.Builds(a)
	code := a.Run(context.Background(), slices.Concat([]string{"org"}, args, []string{"--base", srv.URL}))
	r := result{stderr: stderr.String(), code: code, built: rec.Log()}
	if b, ok := stdout.(*bytes.Buffer); ok {
		r.stdout = b.String()
	}
	return r
}

// ok fails the test unless r succeeded, having built what an org command
// declares.
func (r result) ok(t *testing.T) {
	t.Helper()
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
	}
	if !slices.Equal(r.built, built) {
		t.Errorf("nodes built = %q, want %q", r.built, built)
	}
}

// usage fails the test unless r is a usage error reported with want and
// the command's usage, having built nothing and written nothing to stdout.
func (r result) usage(t *testing.T, want string) {
	t.Helper()
	if r.code != process.ExitUsage {
		t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitUsage, r.stderr)
	}
	if !strings.Contains(r.stderr, want) || !strings.Contains(r.stderr, "\nUsage: slab org ") {
		t.Errorf("stderr = %q, want %q with the command's usage", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if len(r.built) != 0 {
		t.Errorf("nodes built = %q, want none", r.built)
	}
}

// failure fails the test unless r failed with exactly stderr, writing
// nothing to stdout.
func (r result) failure(t *testing.T, stderr string) {
	t.Helper()
	if r.code != process.ExitFailure {
		t.Errorf("code = %d, want %d", r.code, process.ExitFailure)
	}
	if r.stderr != stderr {
		t.Errorf("stderr = %q, want %q", r.stderr, stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
}

func TestCommands_MountsOneSubcommandPerEndpoint(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	for args, wants := range map[string][]string{
		"":     {"list", "get", "get-by-path", "create", "edit", "transfer", "delete", "logo"},
		"logo": {"put", "get", "delete"},
	} {
		r := run(t, srv, strings.Fields(args)...)
		if r.code != process.ExitUsage {
			t.Errorf("org %s: code = %d, want the help's %d", args, r.code, process.ExitUsage)
		}
		for _, want := range wants {
			if !strings.Contains(r.stdout, "\n  "+want+" ") {
				t.Errorf("org %s help lacks %q:\n%s", args, want, r.stdout)
			}
		}
		if len(r.built) != 0 {
			t.Errorf("org %s: nodes built = %q, want none", args, r.built)
		}
	}
}

func TestList_BuildsTheQueryFromTheReadFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		uri  string
	}{
		"no flags":            {nil, "/api/organizations"},
		"page and size":       {[]string{"--page", "3", "--size", "2"}, "/api/organizations?page=3&size=2"},
		"sort":                {[]string{"--sort", "-code,name"}, "/api/organizations?sort=-code%2Cname"},
		"cursor":              {[]string{"--cursor", "eyJrIjoxfQ", "--size", "3", "--sort", "-path"}, "/api/organizations?cursor=eyJrIjoxfQ&size=3&sort=-path"},
		"one filter":          {[]string{"--filter", "code[like]=%o%"}, "/api/organizations?code[like]=%25o%25"},
		"repeated filters":    {[]string{"--filter", "code[like]=%o%", "--filter", "parent_id[null]"}, "/api/organizations?code[like]=%25o%25&parent_id[null]="},
		"filter with equals":  {[]string{"--filter", "name=a=b"}, "/api/organizations?name=a%3Db"},
		"everything together": {[]string{"--page", "1", "--size", "20", "--sort", "code", "--filter", "code=acme"}, "/api/organizations?page=1&size=20&sort=code&code=acme"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"items":[],"page":1,"size":20,"total":0}`)
			r := run(t, srv, append([]string{"list"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodGet || got.uri != tc.uri {
				t.Errorf("request = %s %s, want GET %s", got.method, got.uri, tc.uri)
			}
			if got.body != "" || got.contentType != "" {
				t.Errorf("a list sent a body %q with Content-Type %q", got.body, got.contentType)
			}
			if want := "{\n  \"items\": [],\n  \"page\": 1,\n  \"size\": 20,\n  \"total\": 0\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestList_RefusesBadReadFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"a filter without a name": {[]string{"--filter", "=x"}, `slab org list: --filter "=x": want field=value`},
		"page and cursor":         {[]string{"--page", "2", "--cursor", "c"}, "slab org list: flags --page, --cursor cannot be used together"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, append([]string{"list"}, tc.args...)...).usage(t, tc.want)
			if len(s.received) != 0 {
				t.Errorf("the request fired anyway: %+v", s.received)
			}
		})
	}
}

func TestGet_ReadsById(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{"id":"a1","code":"acme"}`)
	r := run(t, srv, "get", "a1")
	r.ok(t)
	if got := s.only(t); got.method != http.MethodGet || got.uri != "/api/organizations/a1" {
		t.Errorf("request = %s %s", got.method, got.uri)
	}
	if want := "{\n  \"id\": \"a1\",\n  \"code\": \"acme\"\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
}

func TestGet_EscapesTheId(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	run(t, srv, "get", "a/b").ok(t)
	if got := s.only(t); got.uri != "/api/organizations/a%2Fb" {
		t.Errorf("uri = %s, want the slash escaped", got.uri)
	}
}

// A problem response is reported once, every member of the document on
// its own line after the command's path, with nothing on stdout.
func TestGet_ReportsTheProblemAnErrorStatusCarries(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		reply  string
		want   string
	}{
		"status and detail": {
			http.StatusNotFound,
			`{"type":"about:blank","title":"Not Found","status":404,"detail":"no such row"}`,
			"slab org get: 404 Not Found\ndetail: no such row\n",
		},
		"every member": {
			http.StatusPreconditionFailed,
			`{"type":"https://example.test/problems/stale","title":"Precondition Failed","status":412,"detail":"If-Match names version 3, the row is at 4","instance":"/api/organizations/a1","request_id":"abc","attempt":2,"hint":{"fetch":true}}`,
			"slab org get: 412 Precondition Failed\n" +
				"detail: If-Match names version 3, the row is at 4\n" +
				"instance: /api/organizations/a1\n" +
				"type: https://example.test/problems/stale\n" +
				"attempt: 2\n" +
				"hint: {\"fetch\":true}\n" +
				"request_id: abc\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, srv := newService(t, tc.status, tc.reply)
			run(t, srv, "get", "a1").failure(t, tc.want)
		})
	}
}

// A response that carries the wrong status and no problem document says
// which status it wanted.
func TestCreate_ExpectsCreated(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{"id":"n1","version":1}`)
	r := run(t, srv, "create", "--code", "sales", "--name", "Sales")
	if r.code != process.ExitFailure || !strings.HasPrefix(r.stderr, "slab org create: ") || !strings.Contains(r.stderr, "want 201") {
		t.Errorf("code = %d, stderr = %q, want the status mismatch", r.code, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
}

func TestGetByPath_ReadsByHierarchyPath(t *testing.T) {
	for _, path := range []string{"acme/engineering", "/acme/engineering"} {
		s, srv := newService(t, http.StatusOK, `{"path":"/acme/engineering"}`)
		run(t, srv, "get-by-path", path).ok(t)
		if got := s.only(t); got.method != http.MethodGet || got.uri != "/api/organizations/lookup?path=%2Facme%2Fengineering" {
			t.Errorf("get-by-path %q sent %s %s", path, got.method, got.uri)
		}
	}
}

func TestCreate_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"root":         {[]string{"--code", "sales", "--name", "Sales"}, `{"parent_id":null,"code":"sales","name":"Sales"}`},
		"under parent": {[]string{"--code", "sales", "--name", "Sales", "--parent-id", "p1"}, `{"parent_id":"p1","code":"sales","name":"Sales"}`},
		"empty parent": {[]string{"--code", "sales", "--name", "Sales", "--parent-id="}, `{"parent_id":null,"code":"sales","name":"Sales"}`},
		"verbatim":     {[]string{"--body", `{ "code":"sales" , "name":"Sales", "parent_id": null }`}, `{ "code":"sales" , "name":"Sales", "parent_id": null }`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{"id":"n1","version":1}`)
			r := run(t, srv, append([]string{"create"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/api/organizations" {
				t.Errorf("request = %s %s, want POST /api/organizations", got.method, got.uri)
			}
			if got.body != tc.body {
				t.Errorf("body = %s, want %s", got.body, tc.body)
			}
			if got.contentType != "application/json" || got.ifMatch != "" {
				t.Errorf("Content-Type = %q, If-Match = %q", got.contentType, got.ifMatch)
			}
			if want := "{\n  \"id\": \"n1\",\n  \"version\": 1\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestCreate_RefusesBodyAlongsideAFieldFlag(t *testing.T) {
	for _, flag := range []string{"code", "name", "parent-id"} {
		t.Run(flag, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{}`)
			run(t, srv, "create", "--body", `{}`, "--"+flag, "x").usage(t, "slab org create: flags --body, --"+flag+" cannot be used together")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestCreate_RequiresCodeAndNameOnTheFlagsPath(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":   {},
		"code only": {"--code", "sales"},
		"name only": {"--name", "Sales"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{}`)
			run(t, srv, append([]string{"create"}, args...)...).usage(t, "slab org create: --code and --name are required unless --body is given")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestEdit_ResolvesTheVersionFromTheFlagOrTheBody(t *testing.T) {
	for name, tc := range map[string]struct {
		args    []string
		ifMatch string
		body    string
	}{
		"flags path": {
			[]string{"--code", "finance", "--name", "Finance and Accounting", "--version", "3"},
			`"3"`, `{"code":"finance","name":"Finance and Accounting"}`,
		},
		"body carries the version": {
			[]string{"--body", `{"code":"x","name":"y","version":4}`},
			`"4"`, `{"code":"x","name":"y"}`,
		},
		"flag wins over the body and the key is still stripped": {
			[]string{"--body", `{"code":"x","name":"y","version":4}`, "--version", "9"},
			`"9"`, `{"code":"x","name":"y"}`,
		},
		"flag with a body lacking the key sends the body untouched": {
			[]string{"--body", `{ "code": "x", "name": "y" }`, "--version", "2"},
			`"2"`, `{ "code": "x", "name": "y" }`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"id":"e1","version":5}`)
			r := run(t, srv, append([]string{"edit", "e1"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPut || got.uri != "/api/organizations/e1" {
				t.Errorf("request = %s %s, want PUT /api/organizations/e1", got.method, got.uri)
			}
			if got.ifMatch != tc.ifMatch {
				t.Errorf("If-Match = %s, want %s", got.ifMatch, tc.ifMatch)
			}
			if got.body != tc.body {
				t.Errorf("body = %s, want %s", got.body, tc.body)
			}
			if want := "{\n  \"id\": \"e1\",\n  \"version\": 5\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestEdit_RefusesARequestWithoutAVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"flags path":                 {[]string{"--code", "x", "--name", "y"}, "required flag --version not set"},
		"body without key":           {[]string{"--body", `{"code":"x","name":"y"}`}, "--version is required"},
		"body with a string":         {[]string{"--body", `{"code":"x","name":"y","version":"4"}`}, `"version" is not an integer`},
		"body not an object":         {[]string{"--body", `[1]`}, "--body is not a JSON object"},
		"flags path missing a field": {[]string{"--code", "x", "--version", "1"}, "--code and --name are required"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, append([]string{"edit", "e1"}, tc.args...)...).usage(t, tc.want)
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestEdit_RefusesBodyAlongsideAFieldFlag(t *testing.T) {
	for _, flag := range []string{"code", "name"} {
		t.Run(flag, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, "edit", "e1", "--body", `{"version":1}`, "--"+flag, "x").usage(t, "slab org edit: flags --body, --"+flag+" cannot be used together")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestTransfer_DistinguishesAnEmptyParentFromAnUnsetOne(t *testing.T) {
	for name, tc := range map[string]struct {
		args    []string
		ifMatch string
		body    string
	}{
		"to a parent":       {[]string{"--parent-id", "p2", "--version", "1"}, `"1"`, `{"parent_id":"p2"}`},
		"to the root":       {[]string{"--parent-id=", "--version", "1"}, `"1"`, `{"parent_id":null}`},
		"body with version": {[]string{"--body", `{"parent_id":null,"version":5}`}, `"5"`, `{"parent_id":null}`},
		"body as written":   {[]string{"--body", `{}`, "--version", "1"}, `"1"`, `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"id":"t1","version":2}`)
			r := run(t, srv, append([]string{"transfer", "t1"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/api/organizations/t1/transfer" {
				t.Errorf("request = %s %s, want POST /api/organizations/t1/transfer", got.method, got.uri)
			}
			if got.ifMatch != tc.ifMatch {
				t.Errorf("If-Match = %s, want %s", got.ifMatch, tc.ifMatch)
			}
			if got.body != tc.body {
				t.Errorf("body = %s, want %s", got.body, tc.body)
			}
			if want := "{\n  \"id\": \"t1\",\n  \"version\": 2\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestTransfer_RefusesBadInput(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"an unset parent":                  {[]string{"--version", "1"}, "slab org transfer: --parent-id is required unless --body is given"},
		"no version on the flags path":     {[]string{"--parent-id", "p2"}, "slab org transfer: required flag --version not set"},
		"no version on the body path":      {[]string{"--body", `{"parent_id":"p2"}`}, "slab org transfer: --version is required"},
		"a body alongside the parent":      {[]string{"--body", `{"version":1}`, "--parent-id", "p2"}, "slab org transfer: flags --body, --parent-id cannot be used together"},
		"a version that is not an integer": {[]string{"--parent-id", "p2", "--version", "x"}, "slab org transfer: invalid value \"x\" for flag -version"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, append([]string{"transfer", "t1"}, tc.args...)...).usage(t, tc.want)
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestDelete_SendsIfMatchAndPrintsTheStatusLine(t *testing.T) {
	s, srv := newService(t, http.StatusNoContent, "")
	r := run(t, srv, "delete", "d1", "--version", "2")
	r.ok(t)
	got := s.only(t)
	if got.method != http.MethodDelete || got.uri != "/api/organizations/d1" {
		t.Errorf("request = %s %s, want DELETE /api/organizations/d1", got.method, got.uri)
	}
	if got.ifMatch != `"2"` || got.body != "" || got.contentType != "" {
		t.Errorf("If-Match = %s, body = %q, Content-Type = %q", got.ifMatch, got.body, got.contentType)
	}
	if r.stdout != "204 No Content\n" {
		t.Errorf("stdout = %q, want the status line", r.stdout)
	}
}

func TestDelete_RequiresTheVersionFlag(t *testing.T) {
	s, srv := newService(t, http.StatusNoContent, "")
	run(t, srv, "delete", "d1").usage(t, "slab org delete: required flag --version not set")
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

func TestPositionalArguments_AreRequiredWhereTheRouteHasAnId(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"get-by-path"}, {"edit"}, {"transfer"}, {"delete"}, {"logo", "get"}, {"logo", "delete"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, args...).usage(t, "slab org "+strings.Join(args, " ")+": accepts 1 argument, got 0")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestCommands_RefuseAnUnknownFlag(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	run(t, srv, "get", "a1", "--bogus").usage(t, "slab org get: flag provided but not defined: -bogus")
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

// Every org command builds the two nodes it declares, config and client,
// and the lifecycle configuration, and nothing else.
func TestCommands_BuildOnlyTheConfigAndTheClient(t *testing.T) {
	png := filepath.Join(t.TempDir(), "acme.png")
	if err := os.WriteFile(png, []byte("\x89PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"list"},
		{"get", "a1"},
		{"get-by-path", "acme"},
		{"create", "--code", "c", "--name", "n"},
		{"edit", "e1", "--code", "c", "--name", "n", "--version", "1"},
		{"transfer", "t1", "--parent-id=", "--version", "1"},
		{"delete", "d1", "--version", "1"},
		{"logo", "put", "a1", png},
		{"logo", "get", "a1"},
		{"logo", "delete", "a1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, srv := newService(t, http.StatusTeapot, `{"status":418}`)
			r := run(t, srv, args...)
			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want the problem's %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if !slices.Equal(r.built, built) {
				t.Errorf("nodes built = %q, want %q", r.built, built)
			}
		})
	}
}

// The client binds to the base URL the config resolves, from SLAB_BASE
// when no --base is given.
func TestCommands_BindTheClientToSLABBase(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	t.Setenv(app.NewConfigEnv("SLAB").Base, srv.URL)
	var stdout, stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if code := a.Run(context.Background(), []string{"org", "get", "a1"}); code != process.ExitOK {
		t.Fatalf("code = %d; stderr = %q", code, stderr.String())
	}
	if got := s.only(t); got.uri != "/api/organizations/a1" {
		t.Errorf("uri = %s", got.uri)
	}
}

// A success is colored on a terminal unless --no-color, SLAB_NO_COLOR, or
// NO_COLOR says otherwise. The terminal is the test's own: under go test
// the process's stdout is never one.
func TestCommands_ColorTheResultOnATerminal(t *testing.T) {
	on := style.New(true)
	colored := on.Key(`"code"`) + ": " + on.Value(`"acme"`)
	for name, tc := range map[string]struct {
		args    []string
		env     map[string]string
		colored bool
	}{
		"by default":    {nil, nil, true},
		"--no-color":    {[]string{"--no-color"}, nil, false},
		"SLAB_NO_COLOR": {nil, map[string]string{app.NewConfigEnv("SLAB").NoColor: "true"}, false},
		"NO_COLOR":      {nil, map[string]string{"NO_COLOR": "1"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, srv := newService(t, http.StatusOK, `{"code":"acme"}`)
			term := termtest.Open(t)
			r := runTo(t, term.Writer(), srv, append([]string{"get", "a1"}, tc.args...)...)
			r.ok(t)
			out := term.Output()
			if got := strings.Contains(out, colored); got != tc.colored {
				t.Errorf("stdout colored = %v, want %v:\n%q", got, tc.colored, out)
			}
			if !strings.Contains(out, `"code"`) {
				t.Errorf("stdout lacks the rendered body:\n%q", out)
			}
		})
	}
}

// A stream that is no terminal is never colored, whatever the flags say.
func TestCommands_NeverColorAStreamThatIsNoTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	_, srv := newService(t, http.StatusOK, `{"code":"acme"}`)
	r := run(t, srv, "get", "a1")
	r.ok(t)
	if want := "{\n  \"code\": \"acme\"\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want the plain body %q", r.stdout, want)
	}
}

// logoOrg is an organization id the logo commands address.
const logoOrg = "00000000-0000-7000-8000-000000000001"

// logo put sends the file's bytes under the media type its extension
// names, or the one --content-type gives.
func TestLogoPut_SendsTheFileUnderItsMediaType(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "acme.png")
	if err := os.WriteFile(png, []byte("\x89PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		args        []string
		contentType string
	}{
		"by extension": {[]string{"logo", "put", logoOrg, png}, "image/png"},
		"named":        {[]string{"logo", "put", logoOrg, png, "--content-type", "image/webp"}, "image/webp"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{"id":"f","version":2}`)
			run(t, srv, tc.args...).ok(t)
			got := s.only(t)
			if got.method != http.MethodPut || got.uri != "/api/organizations/"+logoOrg+"/logo" || got.contentType != tc.contentType || got.body != "\x89PNG" {
				t.Errorf("sent %s %s as %q: %q", got.method, got.uri, got.contentType, got.body)
			}
		})
	}
}

func TestLogoPut_RefusesAnUnknownExtension(t *testing.T) {
	file := filepath.Join(t.TempDir(), "logo.nope")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, srv := newService(t, http.StatusCreated, `{}`)
	run(t, srv, "logo", "put", logoOrg, file).usage(t, "no media type for its extension; name one with --content-type")
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

// A file that cannot be read is a failure, not a usage error.
func TestLogoPut_FailsOnAnUnreadableFile(t *testing.T) {
	s, srv := newService(t, http.StatusCreated, `{}`)
	r := run(t, srv, "logo", "put", logoOrg, filepath.Join(t.TempDir(), "missing.png"))
	if r.code != process.ExitFailure || !strings.HasPrefix(r.stderr, "slab org logo put: ") {
		t.Errorf("code = %d, stderr = %q, want the read failure", r.code, r.stderr)
	}
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

// logo get never prints the bytes: it prints the headers and writes the
// bytes to --out; a revalidation is a 304, not an error.
func TestLogoGet_WritesTheBytesToOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"e1"` {
			w.Header().Set("ETag", `"e1"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"e1"`)
		_, _ = w.Write([]byte("\x89PNG"))
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "out.png")

	r := run(t, srv, "logo", "get", logoOrg, "--out", file)
	r.ok(t)
	if b, _ := os.ReadFile(file); string(b) != "\x89PNG" || !strings.Contains(r.stdout, "ETag: \"e1\"") || !strings.Contains(r.stdout, "4 bytes written to") || strings.Contains(r.stdout, "PNG") {
		t.Errorf("out = %q, file = %q", r.stdout, b)
	}
	r = run(t, srv, "logo", "get", logoOrg, "--if-none-match", `"e1"`)
	r.ok(t)
	if !strings.HasPrefix(r.stdout, "304 Not Modified") {
		t.Errorf("revalidation = %q", r.stdout)
	}
}

func TestLogoDelete_ExpectsNoContent(t *testing.T) {
	s, srv := newService(t, http.StatusNoContent, "")
	r := run(t, srv, "logo", "delete", logoOrg)
	r.ok(t)
	if r.stdout != "204 No Content\n" {
		t.Errorf("stdout = %q, want the status line", r.stdout)
	}
	if got := s.only(t); got.method != http.MethodDelete || got.uri != "/api/organizations/"+logoOrg+"/logo" {
		t.Errorf("sent %s %s", got.method, got.uri)
	}
}
