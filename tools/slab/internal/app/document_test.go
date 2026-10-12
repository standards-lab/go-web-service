package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
)

// The organization and the ids the commands address.
const (
	org  = "00000000-0000-7000-8000-000000000001"
	dir  = "00000000-0000-7000-8000-000000000002"
	file = "00000000-0000-7000-8000-000000000003"
)

// runDocs runs a fresh App over "docs" and args, with --base naming
// srv, and returns what it did.
func runDocs(t *testing.T, srv *httptest.Server, args ...string) result {
	t.Helper()
	return runFamily(t, nil, []string{"docs"}, srv, args...)
}

// Run without a leaf, docs and each group print their help, listing the
// subcommands, as a usage error that builds nothing.
func TestDocs_MountsOneSubcommandPerEndpoint(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	for args, wants := range map[string][]string{
		"":      {"dirs", "files"},
		"dirs":  {"create", "get", "list", "move", "delete"},
		"files": {"list", "put", "show", "get", "move", "delete"},
	} {
		r := runDocs(t, srv, strings.Fields(args)...)
		if r.code != process.ExitUsage {
			t.Errorf("docs %s: code = %d, want the help's %d", args, r.code, process.ExitUsage)
		}
		for _, want := range wants {
			if !strings.Contains(r.stdout, "\n  "+want+" ") {
				t.Errorf("docs %s help lacks %q:\n%s", args, want, r.stdout)
			}
		}
		if len(r.built) != 0 {
			t.Errorf("docs %s: nodes built = %q, want none", args, r.built)
		}
	}
}

// Every command sends its one request: the method, the URI with each
// segment escaped, and the body and headers the flags build. An upload's
// raw body is covered by the put tests below.
func TestDocs_SendTheRequestTheirRouteNames(t *testing.T) {
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
			runDocs(t, srv, tc.args...).ok(t)
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

func TestDocs_RenderTheReply(t *testing.T) {
	s, srv := newService(t, http.StatusCreated, `{"id":"n1","version":1}`)
	r := runDocs(t, srv, "dirs", "create", org, "--parent-id", "root", "--name", "reports")
	r.ok(t)
	s.only(t)
	if want := "{\n  \"id\": \"n1\",\n  \"version\": 1\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	_, srv = newService(t, http.StatusNoContent, "")
	r = runDocs(t, srv, "files", "delete", org, file, "--version", "1")
	r.ok(t)
	if r.stdout != "204 No Content\n" {
		t.Errorf("delete = %q; want the status line", r.stdout)
	}
}

func TestDocs_ExpectTheirOneStatus(t *testing.T) {
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
		t.Run(name, func(t *testing.T) {
			_, srv := newService(t, http.StatusAccepted, `{}`)
			r := runDocs(t, srv, tc.args...)
			r.failed(t, strings.Join(tc.args[:2], " "), tc.want)
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
		})
	}
}

// A problem response is reported once, every member of the document on
// its own line after the command's path, with nothing on stdout.
func TestDocs_ReportTheProblemAnErrorStatusCarries(t *testing.T) {
	_, srv := newService(t, http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404,"detail":"no such directory"}`)
	runDocs(t, srv, "dirs", "get", org, dir).failure(t, "slab docs dirs get: 404 Not Found\ndetail: no such directory\n")
}

// Every input refusal is a usage error, before anything is built or the
// request fires.
func TestDocs_RefuseMissingInputBeforeAnyRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"dirs create nothing":     {[]string{"dirs", "create", org}, "slab docs dirs create: --parent-id and --name are required unless --body is given"},
		"dirs create no name":     {[]string{"dirs", "create", org, "--parent-id", "root"}, "--parent-id and --name are required"},
		"dirs create no parent":   {[]string{"dirs", "create", org, "--name", "x"}, "--parent-id and --name are required"},
		"dirs move no version":    {[]string{"dirs", "move", org, dir, "--parent-id", "root", "--name", "x"}, "slab docs dirs move: required flag --version not set"},
		"dirs move no name":       {[]string{"dirs", "move", org, dir, "--version", "1", "--parent-id", "root"}, "--parent-id and --name are required"},
		"dirs move body no key":   {[]string{"dirs", "move", org, dir, "--body", `{"parent_id":"root","name":"x"}`}, "--version is required"},
		"files move no version":   {[]string{"files", "move", org, file, "--directory-id", "root", "--name", "x"}, "slab docs files move: required flag --version not set"},
		"files move no dir":       {[]string{"files", "move", org, file, "--version", "1", "--name", "x"}, "--directory-id and --name are required"},
		"files move body no key":  {[]string{"files", "move", org, file, "--body", `{"directory_id":"root","name":"x"}`}, "--version is required"},
		"dirs delete no version":  {[]string{"dirs", "delete", org, dir, "--recursive"}, "slab docs dirs delete: required flag --version not set"},
		"files delete no version": {[]string{"files", "delete", org, file}, "slab docs files delete: required flag --version not set"},
		"wait without recursive":  {[]string{"dirs", "delete", org, dir, "--version", "1", "--wait", "5s"}, "slab docs dirs delete: --wait needs --recursive"},
		"negative wait":           {[]string{"dirs", "delete", org, dir, "--version", "1", "--recursive", "--wait", "-1s"}, "slab docs dirs delete: --wait -1s: want a positive duration"},
		"list nameless filter":    {[]string{"files", "list", org, "root", "--filter", "=x"}, `slab docs files list: --filter "=x": want field=value`},
		"list page and cursor":    {[]string{"dirs", "list", org, "root", "--page", "2", "--cursor", "c"}, "slab docs dirs list: flags --page, --cursor cannot be used together"},
		"an unknown flag":         {[]string{"dirs", "get", org, dir, "--bogus"}, "slab docs dirs get: flag provided but not defined: -bogus"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			runDocs(t, srv, tc.args...).usage(t, tc.want)
			if len(s.received) != 0 {
				t.Errorf("the request fired anyway: %+v", s.received)
			}
		})
	}
}

// A file that cannot be read is a failure, not a usage error, and no
// request fires.
func TestFilesPut_FailsOnAnUnreadableFile(t *testing.T) {
	s, srv := newService(t, http.StatusCreated, `{}`)
	runDocs(t, srv, "files", "put", org, "root", filepath.Join(t.TempDir(), "missing")).failed(t, "files put", "no such file")
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

func TestDocs_RefuseBodyAlongsideAFieldFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		args  []string
		flags string
	}{
		"dirs create parent": {[]string{"dirs", "create", org, "--body", `{}`, "--parent-id", "root"}, "--body, --parent-id"},
		"dirs create name":   {[]string{"dirs", "create", org, "--body", `{}`, "--name", "x"}, "--body, --name"},
		"dirs move parent":   {[]string{"dirs", "move", org, dir, "--body", `{"version":1}`, "--parent-id", "root"}, "--body, --parent-id"},
		"dirs move name":     {[]string{"dirs", "move", org, dir, "--body", `{"version":1}`, "--name", "x"}, "--body, --name"},
		"files move dir":     {[]string{"files", "move", org, file, "--body", `{"version":1}`, "--directory-id", "root"}, "--body, --directory-id"},
		"files move name":    {[]string{"files", "move", org, file, "--body", `{"version":1}`, "--name", "x"}, "--body, --name"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			runDocs(t, srv, tc.args...).usage(t, "flags "+tc.flags+" cannot be used together")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestPositionalArguments_AreRequiredForEveryPathSegment(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"dirs", "create"}, "accepts 1 argument, got 0"},
		{[]string{"dirs", "get", org}, "accepts 2 arguments, got 1"},
		{[]string{"dirs", "list", org}, "accepts 2 arguments, got 1"},
		{[]string{"dirs", "move", org}, "accepts 2 arguments, got 1"},
		{[]string{"dirs", "delete", org}, "accepts 2 arguments, got 1"},
		{[]string{"files", "list", org}, "accepts 2 arguments, got 1"},
		{[]string{"files", "put", org, "root"}, "accepts 3 arguments, got 2"},
		{[]string{"files", "show", org}, "accepts 2 arguments, got 1"},
		{[]string{"files", "get", org}, "accepts 2 arguments, got 1"},
		{[]string{"files", "move", org}, "accepts 2 arguments, got 1"},
		{[]string{"files", "delete", org}, "accepts 2 arguments, got 1"},
	} {
		t.Run(strings.Join(tc.args[:2], " "), func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			runDocs(t, srv, tc.args...).usage(t, "slab docs "+strings.Join(tc.args[:2], " ")+": "+tc.want)
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

// Every docs command builds the nodes it declares, no color and
// client, the base node the client is bound to, and the lifecycle
// configuration, and nothing else.
func TestDocs_BuildOnlyTheColorTheClientAndItsBase(t *testing.T) {
	upload := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"dirs", "create", org, "--parent-id", "root", "--name", "x"},
		{"dirs", "get", org, dir},
		{"dirs", "list", org, dir},
		{"dirs", "move", org, dir, "--version", "1", "--parent-id", "root", "--name", "x"},
		{"dirs", "delete", org, dir, "--version", "1"},
		{"files", "list", org, dir},
		{"files", "put", org, dir, upload},
		{"files", "show", org, file},
		{"files", "get", org, file},
		{"files", "move", org, file, "--version", "1", "--directory-id", "root", "--name", "x"},
		{"files", "delete", org, file, "--version", "1"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			_, srv := newService(t, http.StatusTeapot, `{"status":418}`)
			r := runDocs(t, srv, args...)
			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want the problem's %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if !slices.Equal(r.built, familyBuilt) {
				t.Errorf("nodes built = %q, want %q", r.built, familyBuilt)
			}
		})
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
	base := "/api/documents/" + org + "/directories/root/files?name="
	for name, tc := range map[string]struct {
		args        []string
		uri         string
		contentType string
	}{
		"by extension":   {[]string{pdf}, base + "q3.pdf", "application/pdf"},
		"unknown":        {[]string{unknown}, base + "notes.nope", "application/octet-stream"},
		"named type":     {[]string{unknown, "--content-type", "text/markdown"}, base + "notes.nope", "text/markdown"},
		"named, escaped": {[]string{pdf, "--name", "Q3 report/final.pdf"}, base + "Q3+report%2Ffinal.pdf", "application/pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusCreated, `{"id":"f","version":1}`)
			runDocs(t, srv, append([]string{"files", "put", org, "root"}, tc.args...)...).ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != tc.uri || got.contentType != tc.contentType || got.body != "bytes" {
				t.Errorf("sent %s %s as %q: %q; want POST %s as %q", got.method, got.uri, got.contentType, got.body, tc.uri, tc.contentType)
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

	r := runDocs(t, srv, "files", "get", org, file, "--out", path)
	r.ok(t)
	out := r.stdout
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
	r = runDocs(t, srv, "files", "get", org, file, "--if-none-match", `"e1"`, "--out", unsaved)
	r.ok(t)
	if !strings.HasPrefix(r.stdout, "304 Not Modified") {
		t.Errorf("revalidation = %q", r.stdout)
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
	r := runDocs(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive")
	r.ok(t)
	want := "202 Accepted\n" +
		"Location: " + loc + "\n" +
		"the branch is deleting; the sweep removes it, and the Location answers 404 once it has\n"
	if r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}

	_, srv = newService(t, http.StatusNoContent, "")
	runDocs(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive").failed(t, "dirs delete", "want 202")
	_, srv = newService(t, http.StatusAccepted, "")
	runDocs(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive").failed(t, "dirs delete", "no Location")
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
	r := runDocs(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive", "--wait", "10s")
	r.ok(t)
	out := r.stdout
	want := []string{"DELETE " + loc + "?recursive=true", "GET " + loc, "GET " + loc}
	if strings.Join(*seen, ",") != strings.Join(want, ",") {
		t.Errorf("requests = %q, want %q", *seen, want)
	}
	if !strings.HasPrefix(out, "202 Accepted\n") || !strings.Contains(out, "\n404 Not Found\nthe sweep removed the branch within ") {
		t.Errorf("stdout = %q, want the 202 then the 404", out)
	}

	srv, _ = sweeping(t, 1000)
	runDocs(t, srv, "dirs", "delete", org, dir, "--version", "4", "--recursive", "--wait", "100ms").failed(t, "dirs delete", "still answers 200 after 100ms")
}

// dirs get prints the read as the service wrote it, its status among the
// members, and the entity restates the member.
func TestDirsGet_ShowsTheStatus(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{"id":"`+dir+`","parent_id":null,"name":"reports","path":"/reports","status":"deleting","version":5}`)
	r := runDocs(t, srv, "dirs", "get", org, dir)
	r.ok(t)
	if out := r.stdout; !strings.Contains(out, `"status": "deleting"`) {
		t.Errorf("stdout lacks the status:\n%s", out)
	}
	var d document.Directory
	if err := json.Unmarshal([]byte(`{"id":"x","status":"deleting"}`), &d); err != nil || d.Status != document.DirectoryDeleting {
		t.Errorf("Directory = %+v, %v; want status deleting", d, err)
	}
}

// The guarded deletes' refusals are reported as the service wrote them,
// after the command's path: the status and title, then the detail.
func TestDeletes_ReportThePreconditionAndConflictProblems(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		status int
		reply  string
		want   string
	}{
		"428": {
			[]string{"files", "delete", org, file, "--version", "1"}, http.StatusPreconditionRequired,
			`{"title":"Precondition Required","status":428,"detail":"the request requires an If-Match header"}`,
			"slab docs files delete: 428 Precondition Required\ndetail: the request requires an If-Match header\n",
		},
		"412": {
			[]string{"dirs", "delete", org, dir, "--version", "1"}, http.StatusPreconditionFailed,
			`{"title":"Precondition Failed","status":412}`,
			"slab docs dirs delete: 412 Precondition Failed\n",
		},
		"409": {
			[]string{"dirs", "delete", org, dir, "--version", "1"}, http.StatusConflict,
			`{"title":"Conflict","status":409,"detail":"the directory is not empty"}`,
			"slab docs dirs delete: 409 Conflict\ndetail: the directory is not empty\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, srv := newService(t, tc.status, tc.reply)
			runDocs(t, srv, tc.args...).failure(t, tc.want)
		})
	}
}
