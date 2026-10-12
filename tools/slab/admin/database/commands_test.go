package database_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
	contentLength                           int64
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
			method:        r.Method,
			uri:           r.URL.RequestURI(),
			ifMatch:       r.Header.Get("If-Match"),
			contentType:   r.Header.Get("Content-Type"),
			contentLength: r.ContentLength,
			body:          string(body),
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

// built is what every database command builds: the two nodes it declares
// and the lifecycle configuration the dispatcher adds.
var built = []string{"config", "client", "lifecycle config"}

// run runs a fresh App over "admin database" and args, with --base naming
// srv, and returns what it did.
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
	code := a.Run(context.Background(), slices.Concat([]string{"admin", "database"}, args, []string{"--base", srv.URL}))
	r := result{stderr: stderr.String(), code: code, built: rec.Log()}
	if b, ok := stdout.(*bytes.Buffer); ok {
		r.stdout = b.String()
	}
	return r
}

// ok fails the test unless r succeeded, having built what a database
// command declares.
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
	if !strings.Contains(r.stderr, want) || !strings.Contains(r.stderr, "\nUsage: slab admin database ") {
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

// A parent run with no subcommand prints its help, listing its
// subcommands, sends nothing, and builds nothing.
func TestCommands_MountsTheSchemaGroupAndOneSubcommandPerEndpoint(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	for args, wants := range map[string][]string{
		"":       {"diagnostics", "patterns", "schema", "seed", "state", "statements", "states"},
		"schema": {"down", "force", "status", "steps", "up", "verify"},
	} {
		r := run(t, srv, strings.Fields(args)...)
		if r.code != process.ExitUsage {
			t.Errorf("database %s: code = %d, want the help's %d", args, r.code, process.ExitUsage)
		}
		for _, want := range wants {
			if !strings.Contains(r.stdout, "\n  "+want+" ") {
				t.Errorf("database %s help lacks %q:\n%s", args, want, r.stdout)
			}
		}
		if len(r.built) != 0 {
			t.Errorf("database %s: nodes built = %q, want none", args, r.built)
		}
	}
	if len(s.received) != 0 {
		t.Errorf("a parent sent a request: %+v", s.received)
	}
}

// The admin command lists the database and storage families.
func TestAdmin_MountsTheDatabaseAndStorageCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	built := apptest.Builds(a)
	if code := a.Run(context.Background(), []string{"admin"}); code != process.ExitUsage {
		t.Errorf("code = %d, want the help's %d; stderr = %q", code, process.ExitUsage, stderr.String())
	}
	for _, want := range []string{"database", "storage"} {
		if !strings.Contains(stdout.String(), "\n  "+want+" ") {
			t.Errorf("admin help lacks %q:\n%s", want, stdout.String())
		}
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

// The seven endpoints that take no input: each sends its fixed request and
// prints the reply.
func TestFixedCommands_SendTheirRequestWithoutABody(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		method string
		uri    string
	}{
		"schema status": {[]string{"schema", "status"}, http.MethodGet, "/admin/database/schema"},
		"schema verify": {[]string{"schema", "verify"}, http.MethodPost, "/admin/database/schema/verify"},
		"schema up":     {[]string{"schema", "up"}, http.MethodPost, "/admin/database/schema/up"},
		"diagnostics":   {[]string{"diagnostics"}, http.MethodGet, "/admin/database/diagnostics"},
		"patterns":      {[]string{"patterns"}, http.MethodGet, "/admin/database/patterns"},
		"statements":    {[]string{"statements"}, http.MethodGet, "/admin/database/statements"},
		"states":        {[]string{"states"}, http.MethodGet, "/admin/database/states"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":2,"dirty":false}`)
			r := run(t, srv, tc.args...)
			r.ok(t)
			got := s.only(t)
			if got.method != tc.method || got.uri != tc.uri {
				t.Errorf("request = %s %s, want %s %s", got.method, got.uri, tc.method, tc.uri)
			}
			if got.body != "" || got.contentType != "" || got.ifMatch != "" {
				t.Errorf("sent a body %q with Content-Type %q and If-Match %q", got.body, got.contentType, got.ifMatch)
			}
			if want := "{\n  \"version\": 2,\n  \"dirty\": false\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestFixedCommands_RefuseArguments(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	for _, args := range [][]string{
		{"schema", "status", "x"}, {"schema", "verify", "x"}, {"schema", "up", "x"},
		{"diagnostics", "x"}, {"patterns", "x"}, {"statements", "x"}, {"states", "x"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := strings.Join(args[:len(args)-1], " ")
			run(t, srv, args...).usage(t, "slab admin database "+path+": accepts no arguments, got 1")
		})
	}
	if len(s.received) != 0 {
		t.Errorf("a request fired anyway: %+v", s.received)
	}
}

func TestSchemaDown_OmitsStepsWhenUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		args        []string
		body        string
		contentType string
	}{
		"set alone": {[]string{"--set", "app"}, `{"set":"app"}`, "application/json"},
		"steps":     {[]string{"--set", "app", "--steps", "2"}, `{"set":"app","steps":2}`, "application/json"},
		"verbatim":  {[]string{"--body", `{ "set": "app", "steps": 3 }`}, `{ "set": "app", "steps": 3 }`, "application/json"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":1}`)
			r := run(t, srv, append([]string{"schema", "down"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/schema/down" {
				t.Errorf("request = %s %s, want POST /admin/database/schema/down", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != tc.contentType {
				t.Errorf("body = %q, Content-Type = %q; want %q, %q", got.body, got.contentType, tc.body, tc.contentType)
			}
			if want := "{\n  \"version\": 1\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestSchemaSteps_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"positive":                        {[]string{"--set", "app", "--steps", "2"}, `{"set":"app","steps":2}`},
		"negative":                        {[]string{"--set", "app", "--steps", "-1"}, `{"set":"app","steps":-1}`},
		"zero is the service's to refuse": {[]string{"--set", "app", "--steps", "0"}, `{"set":"app","steps":0}`},
		"verbatim":                        {[]string{"--body", `{"set":"app","steps":1}`}, `{"set":"app","steps":1}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":3}`)
			run(t, srv, append([]string{"schema", "steps"}, tc.args...)...).ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/schema/steps" {
				t.Errorf("request = %s %s, want POST /admin/database/schema/steps", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != "application/json" {
				t.Errorf("body = %s, Content-Type = %q; want %s", got.body, got.contentType, tc.body)
			}
		})
	}
}

func TestSchemaForce_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"version":                             {[]string{"--set", "app", "--version", "7"}, `{"set":"app","version":7}`},
		"zero empties":                        {[]string{"--set", "app", "--version", "0"}, `{"set":"app","version":0}`},
		"verbatim":                            {[]string{"--body", `{"set":"app","version":2}`}, `{"set":"app","version":2}`},
		"negative is the service's to refuse": {[]string{"--set", "app", "--version", "-1"}, `{"set":"app","version":-1}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":7}`)
			run(t, srv, append([]string{"schema", "force"}, tc.args...)...).ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/schema/force" {
				t.Errorf("request = %s %s, want POST /admin/database/schema/force", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != "application/json" {
				t.Errorf("body = %s, Content-Type = %q; want %s", got.body, got.contentType, tc.body)
			}
		})
	}
}

func TestSeed_SendsNoBodyWhenStateIsUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		args        []string
		body        string
		contentType string
	}{
		"unset":    {nil, "", ""},
		"state":    {[]string{"--state", "default"}, `{"state":"default"}`, "application/json"},
		"empty":    {[]string{"--state="}, `{"state":""}`, "application/json"},
		"verbatim": {[]string{"--body", `{"state":"empty"}`}, `{"state":"empty"}`, "application/json"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"organizations":7}`)
			r := run(t, srv, append([]string{"seed"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/seed" {
				t.Errorf("request = %s %s, want POST /admin/database/seed", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != tc.contentType {
				t.Errorf("body = %q, Content-Type = %q; want %q, %q", got.body, got.contentType, tc.body, tc.contentType)
			}
			if tc.body == "" && got.contentLength != 0 {
				t.Errorf("Content-Length = %d, want 0 so the service applies its configured set", got.contentLength)
			}
			if want := "{\n  \"organizations\": 7\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestState_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"confirmed":                              {[]string{"--state", "default", "--confirm"}, `{"state":"default","confirm":true}`},
		"unconfirmed is the service's to refuse": {[]string{"--state", "default"}, `{"state":"default","confirm":false}`},
		"verbatim":                               {[]string{"--body", `{"state":"empty","confirm":true}`}, `{"state":"empty","confirm":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"state":"default","seeded":{"organizations":7}}`)
			r := run(t, srv, append([]string{"state"}, tc.args...)...)
			r.ok(t)
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/state" {
				t.Errorf("request = %s %s, want POST /admin/database/state", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != "application/json" || got.ifMatch != "" {
				t.Errorf("body = %s, Content-Type = %q, If-Match = %q; want %s", got.body, got.contentType, got.ifMatch, tc.body)
			}
			if want := "{\n  \"state\": \"default\",\n  \"seeded\": {\n    \"organizations\": 7\n  }\n}\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

// The commands whose body is required refuse the flags path when a
// required field flag is unset, as a usage error, before anything is built
// or the request fires.
func TestRequiredFieldFlags_AreRefusedWhenUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"schema down: set":      {[]string{"schema", "down", "--steps", "1"}, "slab admin database schema down: --set is required unless --body is given"},
		"schema steps: set":     {[]string{"schema", "steps", "--steps", "1"}, "slab admin database schema steps: --set is required unless --body is given"},
		"schema steps: steps":   {[]string{"schema", "steps", "--set", "app"}, "slab admin database schema steps: --steps is required unless --body is given"},
		"schema force: set":     {[]string{"schema", "force", "--version", "1"}, "slab admin database schema force: --set is required unless --body is given"},
		"schema force: version": {[]string{"schema", "force", "--set", "app"}, "slab admin database schema force: --version is required unless --body is given"},
		"state":                 {[]string{"state"}, "slab admin database state: --state is required unless --body is given"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, tc.args...).usage(t, tc.want)
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestBody_IsExclusiveWithTheFieldFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		flag string
	}{
		"schema down":   {[]string{"schema", "down", "--body", `{}`, "--steps", "1"}, "steps"},
		"schema set":    {[]string{"schema", "force", "--body", `{}`, "--set", "app"}, "set"},
		"state confirm": {[]string{"state", "--body", `{}`, "--confirm"}, "confirm"},
		"schema steps":  {[]string{"schema", "steps", "--body", `{}`, "--steps", "1"}, "steps"},
		"schema force":  {[]string{"schema", "force", "--body", `{}`, "--version", "1"}, "version"},
		"seed":          {[]string{"seed", "--body", `{}`, "--state", "x"}, "state"},
		"state":         {[]string{"state", "--body", `{}`, "--state", "x"}, "state"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			run(t, srv, tc.args...).usage(t, "flags --body, --"+tc.flag+" cannot be used together")
			if len(s.received) != 0 {
				t.Error("the request fired anyway")
			}
		})
	}
}

func TestCommands_RefuseAnUnknownFlag(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	run(t, srv, "diagnostics", "--bogus").usage(t, "slab admin database diagnostics: flag provided but not defined: -bogus")
	if len(s.received) != 0 {
		t.Error("the request fired anyway")
	}
}

// A problem response is reported once, every member of the document on
// its own line after the command's path, with nothing on stdout.
func TestCommands_ReportTheProblemAnErrorStatusCarries(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		reply  string
		args   []string
		want   string
	}{
		"a schema operation": {
			http.StatusConflict,
			`{"type":"about:blank","title":"Conflict","status":409,"detail":"dirty at version 2"}`,
			[]string{"schema", "up"},
			"slab admin database schema up: 409 Conflict\ndetail: dirty at version 2\n",
		},
		"a read, member by member": {
			http.StatusNotFound,
			`{"status":404,"title":"Not Found","detail":"no diagnostics","instance":"/admin/database/diagnostics"}`,
			[]string{"diagnostics"},
			"slab admin database diagnostics: 404 Not Found\ndetail: no diagnostics\ninstance: /admin/database/diagnostics\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, srv := newService(t, tc.status, tc.reply)
			run(t, srv, tc.args...).failure(t, tc.want)
		})
	}
}

// A response that carries the wrong status and no problem document says
// which status it wanted.
func TestCommands_ExpectOK(t *testing.T) {
	_, srv := newService(t, http.StatusNoContent, "")
	r := run(t, srv, "seed")
	if r.code != process.ExitFailure || !strings.HasPrefix(r.stderr, "slab admin database seed: ") || !strings.Contains(r.stderr, "want 200") {
		t.Errorf("code = %d, stderr = %q, want the status mismatch", r.code, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
}

// Every database command builds the two nodes it declares, config and
// client, and the lifecycle configuration, and nothing else.
func TestCommands_BuildOnlyTheConfigAndTheClient(t *testing.T) {
	for _, args := range [][]string{
		{"schema", "status"},
		{"schema", "verify"},
		{"schema", "up"},
		{"schema", "down", "--set", "app"},
		{"schema", "steps", "--set", "app", "--steps", "1"},
		{"schema", "force", "--set", "app", "--version", "1"},
		{"seed"},
		{"state", "--state", "default"},
		{"diagnostics"},
		{"patterns"},
		{"statements"},
		{"states"},
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
// when no --base is given: the admin endpoints share the service's base
// URL with the domain ones.
func TestCommands_BindTheClientToSLABBase(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	t.Setenv(app.NewConfigEnv("SLAB").Base, srv.URL)
	var stdout, stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if code := a.Run(context.Background(), []string{"admin", "database", "diagnostics"}); code != process.ExitOK {
		t.Fatalf("code = %d; stderr = %q", code, stderr.String())
	}
	if got := s.only(t); got.uri != "/admin/database/diagnostics" {
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
			r := runTo(t, term.Writer(), srv, append([]string{"diagnostics"}, tc.args...)...)
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
	r := run(t, srv, "diagnostics")
	r.ok(t)
	if want := "{\n  \"code\": \"acme\"\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want the plain body %q", r.stdout, want)
	}
}

// A root flag may come before the admin word, or between the words.
func TestCommands_TakeRootFlagsAtAnyDepth(t *testing.T) {
	for name, args := range map[string][]string{
		"before admin":            {"--base", "%s", "admin", "database", "diagnostics"},
		"with =":                  {"--base=%s", "admin", "database", "diagnostics"},
		"after a boolean flag":    {"--no-color", "--base", "%s", "admin", "database", "diagnostics"},
		"after a value like word": {"--repo", "admin", "--base", "%s", "admin", "database", "diagnostics"},
		"between the words":       {"admin", "--base", "%s", "database", "diagnostics"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{}`)
			for i, a := range args {
				args[i] = strings.ReplaceAll(a, "%s", srv.URL)
			}
			var stdout, stderr bytes.Buffer
			a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
			if code := a.Run(context.Background(), args); code != process.ExitOK {
				t.Fatalf("code = %d; stderr = %q", code, stderr.String())
			}
			if got := s.only(t); got.method != http.MethodGet || got.uri != "/admin/database/diagnostics" {
				t.Errorf("request = %s %s, want GET /admin/database/diagnostics", got.method, got.uri)
			}
		})
	}
}
