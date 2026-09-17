package organization_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
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

// run executes org with args against srv and returns what it wrote to
// stdout and the error it returned.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	org := organization.Commands(func() *organization.Client {
		return organization.NewClient(httpx.NewClient(srv.URL))
	})
	var out, errOut bytes.Buffer
	org.SetOut(&out)
	org.SetErr(&errOut)
	org.SilenceUsage = true
	org.SilenceErrors = true
	org.SetArgs(args)
	err := org.Execute()
	return out.String(), err
}

func TestCommands_MountsOneSubcommandPerEndpoint(t *testing.T) {
	org := organization.Commands(nil)
	if org.Name() != "org" {
		t.Errorf("Commands().Name() = %q; want org", org.Name())
	}
	var names []string
	for _, cmd := range org.Commands() {
		names = append(names, cmd.Name())
	}
	if got := strings.Join(names, ","); got != "create,delete,edit,get,get-by-path,list,transfer" {
		t.Errorf("Commands() subcommands = %s", got)
	}
}

func TestCommands_ConstructsTheClientWhenASubcommandRuns(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	calls := 0
	org := organization.Commands(func() *organization.Client {
		calls++
		return organization.NewClient(httpx.NewClient(srv.URL))
	})
	if calls != 0 {
		t.Fatalf("Commands constructed the client %d times while building the tree", calls)
	}
	org.SetOut(io.Discard)
	org.SetArgs([]string{"list"})
	if err := org.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("the client was constructed %d times, want once at run time", calls)
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
		"one filter":          {[]string{"--filter", "code[like]=%o%"}, "/api/organizations?code[like]=%25o%25"},
		"repeated filters":    {[]string{"--filter", "code[like]=%o%", "--filter", "parent_id[null]"}, "/api/organizations?code[like]=%25o%25&parent_id[null]="},
		"filter with equals":  {[]string{"--filter", "name=a=b"}, "/api/organizations?name=a%3Db"},
		"everything together": {[]string{"--page", "1", "--size", "20", "--sort", "code", "--filter", "code=acme"}, "/api/organizations?page=1&size=20&sort=code&code=acme"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"items":[],"page":1,"size":20,"total":0}`)
			out, err := run(t, srv, append([]string{"list"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != http.MethodGet || got.uri != tc.uri {
				t.Errorf("request = %s %s, want GET %s", got.method, got.uri, tc.uri)
			}
			if got.body != "" || got.contentType != "" {
				t.Errorf("a list sent a body %q with Content-Type %q", got.body, got.contentType)
			}
			if want := "{\n  \"items\": [],\n  \"page\": 1,\n  \"size\": 20,\n  \"total\": 0\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestList_RefusesAFilterWithoutAName(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	_, err := run(t, srv, "list", "--filter", "=x")
	if err == nil || !strings.Contains(err.Error(), "--filter") {
		t.Fatalf("err = %v, want a --filter usage error", err)
	}
	if len(s.received) != 0 {
		t.Errorf("the request fired anyway: %+v", s.received)
	}
}

func TestGet_ReadsById(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{"id":"a1","code":"acme"}`)
	out, err := run(t, srv, "get", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.only(t); got.method != http.MethodGet || got.uri != "/api/organizations/a1" {
		t.Errorf("request = %s %s", got.method, got.uri)
	}
	if want := "{\n  \"id\": \"a1\",\n  \"code\": \"acme\"\n}\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestGet_EscapesTheId(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	if _, err := run(t, srv, "get", "a/b"); err != nil {
		t.Fatal(err)
	}
	if got := s.only(t); got.uri != "/api/organizations/a%2Fb" {
		t.Errorf("uri = %s, want the slash escaped", got.uri)
	}
}

func TestGet_ReturnsTheProblemAnErrorStatusCarries(t *testing.T) {
	_, srv := newService(t, http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404,"detail":"no such row"}`)
	_, err := run(t, srv, "get", "missing")
	var p web.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v (%T), want a web.Problem", err, err)
	}
	if p.Status != 404 || p.Detail != "no such row" {
		t.Errorf("problem = %+v", p)
	}
}

func TestGetByPath_ReadsByHierarchyPath(t *testing.T) {
	for _, path := range []string{"acme/engineering", "/acme/engineering"} {
		s, srv := newService(t, http.StatusOK, `{"path":"/acme/engineering"}`)
		if _, err := run(t, srv, "get-by-path", path); err != nil {
			t.Fatal(err)
		}
		if got := s.only(t); got.method != http.MethodGet || got.uri != "/api/organizations/path/acme/engineering" {
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
			out, err := run(t, srv, append([]string{"create"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
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
			if want := "{\n  \"id\": \"n1\",\n  \"version\": 1\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestCreate_RefusesBodyAlongsideAFieldFlag(t *testing.T) {
	for name, args := range map[string][]string{
		"code":      {"--body", `{}`, "--code", "x"},
		"name":      {"--body", `{}`, "--name", "x"},
		"parent-id": {"--body", `{}`, "--parent-id", "x"},
	} {
		s, srv := newService(t, http.StatusCreated, `{}`)
		_, err := run(t, srv, append([]string{"create"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "if any flags in the group [body "+name+"] are set none of the others can be") {
			t.Errorf("%s: err = %v, want cobra's mutual exclusion error", name, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestCreate_RequiresCodeAndNameOnTheFlagsPath(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":   {},
		"code only": {"--code", "sales"},
		"name only": {"--name", "Sales"},
	} {
		s, srv := newService(t, http.StatusCreated, `{}`)
		_, err := run(t, srv, append([]string{"create"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "--code and --name are required") {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestCreate_ExpectsCreated(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{"id":"n1","version":1}`)
	_, err := run(t, srv, "create", "--code", "sales", "--name", "Sales")
	if err == nil || !strings.Contains(err.Error(), "want 201") {
		t.Fatalf("err = %v, want the status mismatch", err)
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
			out, err := run(t, srv, append([]string{"edit", "e1"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
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
			if want := "{\n  \"id\": \"e1\",\n  \"version\": 5\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestEdit_RefusesARequestWithoutAVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"flags path":                 {[]string{"--code", "x", "--name", "y"}, `required flag(s) "version" not set`},
		"body without key":           {[]string{"--body", `{"code":"x","name":"y"}`}, "--version is required"},
		"body with a string":         {[]string{"--body", `{"code":"x","name":"y","version":"4"}`}, `"version" is not an integer`},
		"body not an object":         {[]string{"--body", `[1]`}, "--body is not a JSON object"},
		"flags path missing a field": {[]string{"--code", "x", "--version", "1"}, "--code and --name are required"},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, append([]string{"edit", "e1"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestEdit_RefusesBodyAlongsideAFieldFlag(t *testing.T) {
	for _, flag := range []string{"code", "name"} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, "edit", "e1", "--body", `{"version":1}`, "--"+flag, "x")
		if err == nil || !strings.Contains(err.Error(), "if any flags in the group [body "+flag+"] are set none of the others can be") {
			t.Errorf("%s: err = %v, want cobra's mutual exclusion error", flag, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", flag)
		}
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
			out, err := run(t, srv, append([]string{"transfer", "t1"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
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
			if want := "{\n  \"id\": \"t1\",\n  \"version\": 2\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestTransfer_RefusesAnUnsetParentOnTheFlagsPath(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	_, err := run(t, srv, "transfer", "t1", "--version", "1")
	if err == nil || !strings.Contains(err.Error(), "--parent-id is required") {
		t.Fatalf("err = %v, want the --parent-id usage error", err)
	}
	if len(s.received) != 0 {
		t.Errorf("the request fired anyway")
	}
}

func TestTransfer_RefusesARequestWithoutAVersion(t *testing.T) {
	for name, args := range map[string][]string{
		"flags path":       {"--parent-id", "p2"},
		"body without key": {"--body", `{"parent_id":"p2"}`},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, append([]string{"transfer", "t1"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "version") {
			t.Errorf("%s: err = %v, want a version usage error", name, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestTransfer_RefusesBodyAlongsideParentId(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	_, err := run(t, srv, "transfer", "t1", "--body", `{"version":1}`, "--parent-id", "p2")
	if err == nil || !strings.Contains(err.Error(), "if any flags in the group [body parent-id] are set none of the others can be") {
		t.Fatalf("err = %v, want cobra's mutual exclusion error", err)
	}
	if len(s.received) != 0 {
		t.Errorf("the request fired anyway")
	}
}

func TestDelete_SendsIfMatchAndPrintsTheStatusLine(t *testing.T) {
	s, srv := newService(t, http.StatusNoContent, "")
	out, err := run(t, srv, "delete", "d1", "--version", "2")
	if err != nil {
		t.Fatal(err)
	}
	got := s.only(t)
	if got.method != http.MethodDelete || got.uri != "/api/organizations/d1" {
		t.Errorf("request = %s %s, want DELETE /api/organizations/d1", got.method, got.uri)
	}
	if got.ifMatch != `"2"` || got.body != "" || got.contentType != "" {
		t.Errorf("If-Match = %s, body = %q, Content-Type = %q", got.ifMatch, got.body, got.contentType)
	}
	if out != "204 No Content\n" {
		t.Errorf("stdout = %q, want the status line", out)
	}
}

func TestDelete_RequiresTheVersionFlag(t *testing.T) {
	s, srv := newService(t, http.StatusNoContent, "")
	_, err := run(t, srv, "delete", "d1")
	if err == nil || !strings.Contains(err.Error(), `required flag(s) "version" not set`) {
		t.Fatalf("err = %v, want cobra's required flag error", err)
	}
	if len(s.received) != 0 {
		t.Errorf("the request fired anyway")
	}
}

func TestPositionalArguments_AreRequiredWhereTheRouteHasAnId(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	for _, name := range []string{"get", "get-by-path", "edit", "transfer", "delete"} {
		if _, err := run(t, srv, name); err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
			t.Errorf("%s without an argument: err = %v", name, err)
		}
	}
}
