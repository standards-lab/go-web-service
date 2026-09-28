package document_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// The organization and the ids the commands address.
const (
	org  = "00000000-0000-7000-8000-000000000001"
	dir  = "00000000-0000-7000-8000-000000000002"
	file = "00000000-0000-7000-8000-000000000003"
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
	location string
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
		if s.location != "" {
			w.Header().Set("Location", s.location)
		}
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

// run executes docs with args against srv and returns what it wrote to
// stdout and the error it returned.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	docs := document.Commands(func() *document.Client {
		return document.NewClient(httpx.NewClient(srv.URL))
	}, output.New(&out, &errOut, nil))
	docs.SetOut(&out)
	docs.SetErr(&errOut)
	docs.SilenceUsage = true
	docs.SilenceErrors = true
	docs.SetArgs(args)
	err := docs.Execute()
	return out.String(), err
}

// names is the subcommands of cmd, in cobra's sorted order.
func names(cmd *cobra.Command) string {
	var all []string
	for _, c := range cmd.Commands() {
		all = append(all, c.Name())
	}
	return strings.Join(all, ",")
}

func TestCommands_MountsOneSubcommandPerEndpoint(t *testing.T) {
	docs := document.Commands(nil, nil)
	if docs.Name() != "docs" {
		t.Errorf("Commands().Name() = %q; want docs", docs.Name())
	}
	if got := names(docs); got != "dirs,files" {
		t.Errorf("docs subcommands = %s", got)
	}
	for _, group := range docs.Commands() {
		want := map[string]string{
			"dirs":  "create,delete,get,list,move",
			"files": "delete,get,list,move,put,show",
		}[group.Name()]
		if got := names(group); got != want {
			t.Errorf("docs %s subcommands = %s, want %s", group.Name(), got, want)
		}
	}
}

func TestCommands_ConstructsTheClientWhenASubcommandRuns(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	calls := 0
	docs := document.Commands(func() *document.Client {
		calls++
		return document.NewClient(httpx.NewClient(srv.URL))
	}, output.New(io.Discard, io.Discard, nil))
	if calls != 0 {
		t.Fatalf("Commands constructed the client %d times while building the tree", calls)
	}
	docs.SetOut(io.Discard)
	docs.SetArgs([]string{"dirs", "get", org, "root"})
	if err := docs.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("the client was constructed %d times, want once at run time", calls)
	}
}

// Every command sends its one request: the method, the URI with each
// segment escaped, and the body and headers the flags build. An upload's
// raw body is covered by the put tests below.
func TestCommands_SendTheRequestTheirRouteNames(t *testing.T) {
	base := "/api/documents/" + org
	for name, tc := range map[string]struct {
		args                              []string
		status                            int
		method, uri, body, ctype, ifMatch string
	}{
		"dirs create": {
			[]string{"dirs", "create", org, "--parent-id", "root", "--name", "reports"}, http.StatusCreated,
			http.MethodPost, base + "/directories", `{"parent_id":"root","name":"reports"}`, "application/json", "",
		},
		"dirs create verbatim": {
			[]string{"dirs", "create", org, "--body", `{ "parent_id":"root", "name":"x" }`}, http.StatusCreated,
			http.MethodPost, base + "/directories", `{ "parent_id":"root", "name":"x" }`, "application/json", "",
		},
		"dirs get": {
			[]string{"dirs", "get", org, dir}, http.StatusOK,
			http.MethodGet, base + "/directories/" + dir, "", "", "",
		},
		"dirs get root": {
			[]string{"dirs", "get", org, "root"}, http.StatusOK,
			http.MethodGet, base + "/directories/root", "", "", "",
		},
		"dirs get escapes": {
			[]string{"dirs", "get", "a/b", "c d"}, http.StatusOK,
			http.MethodGet, "/api/documents/a%2Fb/directories/c%20d", "", "", "",
		},
		"dirs list": {
			[]string{"dirs", "list", org, "root"}, http.StatusOK,
			http.MethodGet, base + "/directories/root/directories", "", "", "",
		},
		"dirs list paged": {
			[]string{"dirs", "list", org, dir, "--page", "2", "--size", "5", "--sort", "-name", "--filter", "name[like]=%r%"}, http.StatusOK,
			http.MethodGet, base + "/directories/" + dir + "/directories?page=2&size=5&sort=-name&name[like]=%25r%25", "", "", "",
		},
		"dirs list cursor": {
			[]string{"dirs", "list", org, "root", "--cursor", "eyJrIjoxfQ", "--size", "3"}, http.StatusOK,
			http.MethodGet, base + "/directories/root/directories?cursor=eyJrIjoxfQ&size=3", "", "", "",
		},
		"dirs move": {
			[]string{"dirs", "move", org, dir, "--version", "3", "--parent-id", "root", "--name", "archive"}, http.StatusOK,
			http.MethodPost, base + "/directories/" + dir + "/move", `{"parent_id":"root","name":"archive"}`, "application/json", `"3"`,
		},
		"dirs move body carries the version": {
			[]string{"dirs", "move", org, dir, "--body", `{"parent_id":"root","name":"a","version":4}`}, http.StatusOK,
			http.MethodPost, base + "/directories/" + dir + "/move", `{"name":"a","parent_id":"root"}`, "application/json", `"4"`,
		},
		"dirs delete": {
			[]string{"dirs", "delete", org, dir, "--version", "3"}, http.StatusNoContent,
			http.MethodDelete, base + "/directories/" + dir, "", "", `"3"`,
		},
		"dirs delete recursive": {
			[]string{"dirs", "delete", org, dir, "--version", "5", "--recursive"}, http.StatusAccepted,
			http.MethodDelete, base + "/directories/" + dir + "?recursive=true", "", "", `"5"`,
		},
		"files list": {
			[]string{"files", "list", org, "root", "--size", "10", "--sort", "name"}, http.StatusOK,
			http.MethodGet, base + "/directories/root/files?size=10&sort=name", "", "", "",
		},
		"files show": {
			[]string{"files", "show", org, file}, http.StatusOK,
			http.MethodGet, base + "/files/" + file, "", "", "",
		},
		"files get": {
			[]string{"files", "get", org, file}, http.StatusOK,
			http.MethodGet, base + "/files/" + file + "/content", "", "", "",
		},
		"files move": {
			[]string{"files", "move", org, file, "--version", "2", "--directory-id", dir, "--name", "q3.pdf"}, http.StatusOK,
			http.MethodPost, base + "/files/" + file + "/move", `{"directory_id":"` + dir + `","name":"q3.pdf"}`, "application/json", `"2"`,
		},
		"files move verbatim": {
			[]string{"files", "move", org, file, "--version", "2", "--body", `{ "directory_id":"root" , "name":"x" }`}, http.StatusOK,
			http.MethodPost, base + "/files/" + file + "/move", `{ "directory_id":"root" , "name":"x" }`, "application/json", `"2"`,
		},
		"files delete": {
			[]string{"files", "delete", org, file, "--version", "2"}, http.StatusNoContent,
			http.MethodDelete, base + "/files/" + file, "", "", `"2"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			reply := `{"id":"x","version":1}`
			if tc.status == http.StatusNoContent || tc.status == http.StatusAccepted {
				reply = ""
			}
			s, srv := newService(t, tc.status, reply)
			if tc.status == http.StatusAccepted {
				s.location = base + "/directories/" + dir
			}
			if _, err := run(t, srv, tc.args...); err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != tc.method || got.uri != tc.uri {
				t.Errorf("request = %s %s, want %s %s", got.method, got.uri, tc.method, tc.uri)
			}
			if got.body != tc.body || got.contentType != tc.ctype || got.ifMatch != tc.ifMatch {
				t.Errorf("body = %q, Content-Type = %q, If-Match = %q; want %q, %q, %q", got.body, got.contentType, got.ifMatch, tc.body, tc.ctype, tc.ifMatch)
			}
		})
	}
}

func TestCommands_RenderTheReply(t *testing.T) {
	s, srv := newService(t, http.StatusCreated, `{"id":"n1","version":1}`)
	out, err := run(t, srv, "dirs", "create", org, "--parent-id", "root", "--name", "reports")
	if err != nil {
		t.Fatal(err)
	}
	s.only(t)
	if want := "{\n  \"id\": \"n1\",\n  \"version\": 1\n}\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	_, srv = newService(t, http.StatusNoContent, "")
	out, err = run(t, srv, "files", "delete", org, file, "--version", "1")
	if err != nil || out != "204 No Content\n" {
		t.Errorf("delete = %q, %v; want the status line", out, err)
	}
}

func TestCommands_ExpectTheirOneStatus(t *testing.T) {
	upload := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"dirs create": {[]string{"dirs", "create", org, "--parent-id", "root", "--name", "x"}, "want 201"},
		"dirs delete": {[]string{"dirs", "delete", org, dir, "--version", "1"}, "want 204"},
		"files put":   {[]string{"files", "put", org, "root", upload}, "want 201"},
	} {
		_, srv := newService(t, http.StatusAccepted, `{}`)
		if _, err := run(t, srv, tc.args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want the status mismatch", name, err)
		}
	}
}

func TestCommands_ReturnTheProblemAnErrorStatusCarries(t *testing.T) {
	_, srv := newService(t, http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404,"detail":"no such directory"}`)
	_, err := run(t, srv, "dirs", "get", org, dir)
	var p web.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v (%T), want a web.Problem", err, err)
	}
	if p.Status != 404 || p.Detail != "no such directory" {
		t.Errorf("problem = %+v", p)
	}
}

// Every field-flag refusal happens before the request fires.
func TestCommands_RefuseMissingInputBeforeAnyRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"dirs create nothing":     {[]string{"dirs", "create", org}, "--parent-id and --name are required"},
		"dirs create no name":     {[]string{"dirs", "create", org, "--parent-id", "root"}, "--parent-id and --name are required"},
		"dirs create no parent":   {[]string{"dirs", "create", org, "--name", "x"}, "--parent-id and --name are required"},
		"dirs move no version":    {[]string{"dirs", "move", org, dir, "--parent-id", "root", "--name", "x"}, `required flag(s) "version" not set`},
		"dirs move no name":       {[]string{"dirs", "move", org, dir, "--version", "1", "--parent-id", "root"}, "--parent-id and --name are required"},
		"dirs move body no key":   {[]string{"dirs", "move", org, dir, "--body", `{"parent_id":"root","name":"x"}`}, "--version is required"},
		"files move no version":   {[]string{"files", "move", org, file, "--directory-id", "root", "--name", "x"}, `required flag(s) "version" not set`},
		"files move no dir":       {[]string{"files", "move", org, file, "--version", "1", "--name", "x"}, "--directory-id and --name are required"},
		"files move body no key":  {[]string{"files", "move", org, file, "--body", `{"directory_id":"root","name":"x"}`}, "--version is required"},
		"dirs delete no version":  {[]string{"dirs", "delete", org, dir, "--recursive"}, `required flag(s) "version" not set`},
		"files delete no version": {[]string{"files", "delete", org, file}, `required flag(s) "version" not set`},
		"wait without recursive":  {[]string{"dirs", "delete", org, dir, "--version", "1", "--wait", "5s"}, "--wait needs --recursive"},
		"negative wait":           {[]string{"dirs", "delete", org, dir, "--version", "1", "--recursive", "--wait", "-1s"}, "--wait -1s"},
		"files put no file":       {[]string{"files", "put", org, "root", filepath.Join(t.TempDir(), "missing")}, "no such file"},
		"list nameless filter":    {[]string{"files", "list", org, "root", "--filter", "=x"}, "--filter"},
		"list page and cursor":    {[]string{"dirs", "list", org, "root", "--page", "2", "--cursor", "c"}, "if any flags in the group [page cursor] are set none of the others can be"},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		if _, err := run(t, srv, tc.args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestCommands_RefuseBodyAlongsideAFieldFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		args  []string
		group string
	}{
		"dirs create parent": {[]string{"dirs", "create", org, "--body", `{}`, "--parent-id", "root"}, "[body parent-id]"},
		"dirs create name":   {[]string{"dirs", "create", org, "--body", `{}`, "--name", "x"}, "[body name]"},
		"dirs move parent":   {[]string{"dirs", "move", org, dir, "--body", `{"version":1}`, "--parent-id", "root"}, "[body parent-id]"},
		"dirs move name":     {[]string{"dirs", "move", org, dir, "--body", `{"version":1}`, "--name", "x"}, "[body name]"},
		"files move dir":     {[]string{"files", "move", org, file, "--body", `{"version":1}`, "--directory-id", "root"}, "[body directory-id]"},
		"files move name":    {[]string{"files", "move", org, file, "--body", `{"version":1}`, "--name", "x"}, "[body name]"},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, tc.args...)
		if err == nil || !strings.Contains(err.Error(), "if any flags in the group "+tc.group+" are set none of the others can be") {
			t.Errorf("%s: err = %v, want cobra's mutual exclusion error", name, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestPositionalArguments_AreRequiredForEveryPathSegment(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"dirs", "create"}, "accepts 1 arg(s), received 0"},
		{[]string{"dirs", "get", org}, "accepts 2 arg(s), received 1"},
		{[]string{"dirs", "list", org}, "accepts 2 arg(s), received 1"},
		{[]string{"dirs", "move", org}, "accepts 2 arg(s), received 1"},
		{[]string{"dirs", "delete", org}, "accepts 2 arg(s), received 1"},
		{[]string{"files", "list", org}, "accepts 2 arg(s), received 1"},
		{[]string{"files", "put", org, "root"}, "accepts 3 arg(s), received 2"},
		{[]string{"files", "show", org}, "accepts 2 arg(s), received 1"},
		{[]string{"files", "get", org}, "accepts 2 arg(s), received 1"},
		{[]string{"files", "move", org}, "accepts 2 arg(s), received 1"},
		{[]string{"files", "delete", org}, "accepts 2 arg(s), received 1"},
	} {
		if _, err := run(t, srv, tc.args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

// files put sends the file's bytes under the stored name, the file's base
// name unless --name gives one and escaped as a path segment, and under the
// media type the extension names, application/octet-stream when it names
// none, or the one --content-type gives.
func TestFilesPut_SendsTheFileUnderItsNameAndMediaType(t *testing.T) {
	tmp := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	pdf, unknown := write("q3.pdf"), write("notes.nope")
	base := "/api/documents/" + org + "/directories/root/files/"
	for name, tc := range map[string]struct {
		args        []string
		uri         string
		contentType string
	}{
		"by extension":   {[]string{pdf}, base + "q3.pdf", "application/pdf"},
		"unknown":        {[]string{unknown}, base + "notes.nope", "application/octet-stream"},
		"named type":     {[]string{unknown, "--content-type", "text/markdown"}, base + "notes.nope", "text/markdown"},
		"named, escaped": {[]string{pdf, "--name", "Q3 report/final.pdf"}, base + "Q3%20report%2Ffinal.pdf", "application/pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{"id":"f","version":1}`)
			if _, err := run(t, srv, append([]string{"files", "put", org, "root"}, tc.args...)...); err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != http.MethodPut || got.uri != tc.uri || got.contentType != tc.contentType || got.body != "bytes" {
				t.Errorf("sent %s %s as %q: %q; want PUT %s as %q", got.method, got.uri, got.contentType, got.body, tc.uri, tc.contentType)
			}
		})
	}
}

// files get never prints the bytes: it prints the headers, the
// attachment's name among them, and writes the bytes to --out; a
// revalidation is a 304, not an error, and writes nothing.
func TestFilesGet_WritesTheBytesToOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"e1"`)
		if r.Header.Get("If-None-Match") == `"e1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="a.txt"`)
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "a.txt")

	out, err := run(t, srv, "files", "get", org, file, "--out", path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "secret" || strings.Contains(out, "secret") {
		t.Errorf("file = %q, stdout = %q; want the bytes in the file only", b, out)
	}
	for _, want := range []string{"200 OK\n", `Content-Disposition: attachment; filename="a.txt"`, `ETag: "e1"`, "6 bytes written to " + path} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}

	unsaved := filepath.Join(t.TempDir(), "b.txt")
	out, err = run(t, srv, "files", "get", org, file, "--if-none-match", `"e1"`, "--out", unsaved)
	if err != nil || !strings.HasPrefix(out, "304 Not Modified") {
		t.Errorf("revalidation = %q, %v", out, err)
	}
	if _, err := os.Stat(unsaved); !os.IsNotExist(err) {
		t.Errorf("a 304 wrote %s: %v", unsaved, err)
	}
}

// A recursive delete is accepted, not done: the command expects 202 and says
// so, with the Location to read and what the sweep will do. A 204 in its
// place is a mismatch, as is a 202 with nothing to follow.
func TestDirsDelete_RecursiveReportsTheAcceptedBranch(t *testing.T) {
	loc := "/api/documents/" + org + "/directories/" + dir
	s, srv := newService(t, http.StatusAccepted, "")
	s.location = loc
	out, err := run(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive")
	if err != nil {
		t.Fatal(err)
	}
	want := "202 Accepted\n" +
		"Location: " + loc + "\n" +
		"the branch is deleting; the sweep removes it, and the Location answers 404 once it has\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}

	_, srv = newService(t, http.StatusNoContent, "")
	if _, err := run(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive"); err == nil || !strings.Contains(err.Error(), "want 202") {
		t.Errorf("a 204 to a recursive delete: err = %v, want the status mismatch", err)
	}
	_, srv = newService(t, http.StatusAccepted, "")
	if _, err := run(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive"); err == nil || !strings.Contains(err.Error(), "no Location") {
		t.Errorf("a 202 without Location: err = %v, want the refusal", err)
	}
}

// sweeping is a fake service whose recursive delete answers 202, and whose
// directory read answers the directory deleting for the first reads reads,
// and 404 after. It returns the requests it received, as method and URI.
func sweeping(t *testing.T, reads int) (*httptest.Server, *[]string) {
	t.Helper()
	loc := "/api/documents/" + org + "/directories/" + dir
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodDelete:
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusAccepted)
		case reads > 0:
			reads--
			w.Header().Set("Content-Type", web.JSONMediaType)
			_, _ = io.WriteString(w, `{"id":"`+dir+`","name":"reports","status":"deleting","version":5}`)
		default:
			w.Header().Set("Content-Type", web.ProblemMediaType)
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"title":"Not Found","status":404}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// --wait reads the Location until it answers 404 and reports the sweep
// done; a Location still answering 200 when the wait runs out is an error.
func TestDirsDelete_WaitPollsTheLocationUntilTheSweepIsDone(t *testing.T) {
	loc := "/api/documents/" + org + "/directories/" + dir
	srv, seen := sweeping(t, 1)
	out, err := run(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive", "--wait", "10s")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DELETE " + loc + "?recursive=true", "GET " + loc, "GET " + loc}
	if strings.Join(*seen, ",") != strings.Join(want, ",") {
		t.Errorf("requests = %q, want %q", *seen, want)
	}
	if !strings.HasPrefix(out, "202 Accepted\n") || !strings.Contains(out, "\n404 Not Found\nthe sweep removed the branch within ") {
		t.Errorf("stdout = %q, want the 202 then the 404", out)
	}

	srv, _ = sweeping(t, 1000)
	if _, err := run(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive", "--wait", "100ms"); err == nil || !strings.Contains(err.Error(), "still answers 200 after 100ms") {
		t.Errorf("err = %v, want the wait to run out", err)
	}
}

// dirs get prints the read as the service wrote it, its status among the
// members, and the entity restates the member.
func TestDirsGet_ShowsTheStatus(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{"id":"`+dir+`","parent_id":null,"name":"reports","path":"/reports","status":"deleting","version":5}`)
	out, err := run(t, srv, "dirs", "get", org, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"status": "deleting"`) {
		t.Errorf("stdout lacks the status:\n%s", out)
	}
	var d document.Directory
	if err := json.Unmarshal([]byte(`{"id":"x","status":"deleting"}`), &d); err != nil || d.Status != document.DirectoryDeleting {
		t.Errorf("Directory = %+v, %v; want status deleting", d, err)
	}
}

// The guarded deletes' refusals print through the root's problem printer
// as the service wrote them: the status and title, then the detail.
func TestDeletes_PrintThePreconditionAndConflictProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		status int
		reply  string
		want   string
	}{
		"428": {
			[]string{"files", "delete", org, file, "--version", "1"}, http.StatusPreconditionRequired,
			`{"title":"Precondition Required","status":428,"detail":"the request requires an If-Match header"}`,
			"428 Precondition Required\ndetail: the request requires an If-Match header\n",
		},
		"412": {
			[]string{"dirs", "delete", org, dir, "--version", "1"}, http.StatusPreconditionFailed,
			`{"title":"Precondition Failed","status":412}`,
			"412 Precondition Failed\n",
		},
		"409": {
			[]string{"dirs", "delete", org, dir, "--version", "1"}, http.StatusConflict,
			`{"title":"Conflict","status":409,"detail":"the directory is not empty"}`,
			"409 Conflict\ndetail: the directory is not empty\n",
		},
	} {
		_, srv := newService(t, tc.status, tc.reply)
		_, err := run(t, srv, tc.args...)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		var printed bytes.Buffer
		output.New(io.Discard, &printed, nil).Error(err)
		if printed.String() != tc.want {
			t.Errorf("%s: printed %q, want %q", name, printed.String(), tc.want)
		}
	}
}
