package database_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
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

// run executes database with args against srv and returns what it wrote to
// stdout and the error it returned.
func run(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	db := database.Commands(func() *database.Client {
		return database.NewClient(httpx.NewClient(srv.URL))
	}, output.New(&out, &errOut, nil))
	db.SetOut(&out)
	db.SetErr(&errOut)
	db.SilenceUsage = true
	db.SilenceErrors = true
	db.SetArgs(args)
	err := db.Execute()
	return out.String(), err
}

func TestCommands_MountsTheSchemaGroupAndOneSubcommandPerEndpoint(t *testing.T) {
	db := database.Commands(nil, nil)
	if db.Name() != "database" {
		t.Errorf("Commands().Name() = %q; want database", db.Name())
	}
	var top []string
	for _, cmd := range db.Commands() {
		top = append(top, cmd.Name())
	}
	if got := strings.Join(top, ","); got != "diagnostics,patterns,schema,seed,state,statements,states" {
		t.Errorf("Commands() subcommands = %s", got)
	}
	schema, _, err := db.Find([]string{"schema"})
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, cmd := range schema.Commands() {
		ops = append(ops, cmd.Name())
	}
	if got := strings.Join(ops, ","); got != "down,force,status,steps,up,verify" {
		t.Errorf("schema subcommands = %s", got)
	}
}

func TestCommands_ConstructsTheClientWhenASubcommandRuns(t *testing.T) {
	_, srv := newService(t, http.StatusOK, `{}`)
	calls := 0
	db := database.Commands(func() *database.Client {
		calls++
		return database.NewClient(httpx.NewClient(srv.URL))
	}, output.New(io.Discard, io.Discard, nil))
	if calls != 0 {
		t.Fatalf("Commands constructed the client %d times while building the tree", calls)
	}
	db.SetOut(io.Discard)
	db.SetArgs([]string{"schema", "status"})
	if err := db.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("the client was constructed %d times, want once at run time", calls)
	}
}

func TestContainers_PrintHelpAndAcceptNoArguments(t *testing.T) {
	s, srv := newService(t, http.StatusOK, `{}`)
	for _, args := range [][]string{{}, {"schema"}} {
		out, err := run(t, srv, args...)
		if err != nil {
			t.Errorf("%v: err = %v", args, err)
		}
		if !strings.Contains(out, "Usage:") {
			t.Errorf("%v: stdout = %q, want the help text", args, out)
		}
	}
	if len(s.received) != 0 {
		t.Errorf("a container sent a request: %+v", s.received)
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
			out, err := run(t, srv, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != tc.method || got.uri != tc.uri {
				t.Errorf("request = %s %s, want %s %s", got.method, got.uri, tc.method, tc.uri)
			}
			if got.body != "" || got.contentType != "" || got.ifMatch != "" {
				t.Errorf("sent a body %q with Content-Type %q and If-Match %q", got.body, got.contentType, got.ifMatch)
			}
			if want := "{\n  \"version\": 2,\n  \"dirty\": false\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
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
		if _, err := run(t, srv, args...); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%v: err = %v, want cobra's no-arguments error", args, err)
		}
	}
	if len(s.received) != 0 {
		t.Errorf("a request fired anyway: %+v", s.received)
	}
}

func TestSchemaDown_SendsNoBodyWhenStepsIsUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		args        []string
		body        string
		contentType string
	}{
		"unset":    {nil, "", ""},
		"steps":    {[]string{"--steps", "2"}, `{"steps":2}`, "application/json"},
		"verbatim": {[]string{"--body", `{ "steps": 3 }`}, `{ "steps": 3 }`, "application/json"},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":1}`)
			out, err := run(t, srv, append([]string{"schema", "down"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/schema/down" {
				t.Errorf("request = %s %s, want POST /admin/database/schema/down", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != tc.contentType {
				t.Errorf("body = %q, Content-Type = %q; want %q, %q", got.body, got.contentType, tc.body, tc.contentType)
			}
			if tc.body == "" && got.contentLength != 0 {
				t.Errorf("Content-Length = %d, want 0 so the service applies its default", got.contentLength)
			}
			if want := "{\n  \"version\": 1\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestSchemaSteps_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"positive":                        {[]string{"--steps", "2"}, `{"steps":2}`},
		"negative":                        {[]string{"--steps", "-1"}, `{"steps":-1}`},
		"zero is the service's to refuse": {[]string{"--steps", "0"}, `{"steps":0}`},
		"verbatim":                        {[]string{"--body", `{"steps":1}`}, `{"steps":1}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":3}`)
			if _, err := run(t, srv, append([]string{"schema", "steps"}, tc.args...)...); err != nil {
				t.Fatal(err)
			}
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
		"version":                             {[]string{"--version", "7"}, `{"version":7}`},
		"zero empties":                        {[]string{"--version", "0"}, `{"version":0}`},
		"verbatim":                            {[]string{"--body", `{"version":2}`}, `{"version":2}`},
		"negative is the service's to refuse": {[]string{"--version", "-1"}, `{"version":-1}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"version":7}`)
			if _, err := run(t, srv, append([]string{"schema", "force"}, tc.args...)...); err != nil {
				t.Fatal(err)
			}
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
			out, err := run(t, srv, append([]string{"seed"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
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
			if want := "{\n  \"organizations\": 7\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestState_SendsTheBodyTheFlagsBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		body string
	}{
		"state":    {[]string{"--state", "default"}, `{"state":"default"}`},
		"verbatim": {[]string{"--body", `{"state":"empty"}`}, `{"state":"empty"}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, srv := newService(t, http.StatusOK, `{"state":"default","seeded":{"organizations":7}}`)
			out, err := run(t, srv, append([]string{"state"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
			got := s.only(t)
			if got.method != http.MethodPost || got.uri != "/admin/database/state" {
				t.Errorf("request = %s %s, want POST /admin/database/state", got.method, got.uri)
			}
			if got.body != tc.body || got.contentType != "application/json" || got.ifMatch != "" {
				t.Errorf("body = %s, Content-Type = %q, If-Match = %q; want %s", got.body, got.contentType, got.ifMatch, tc.body)
			}
			if want := "{\n  \"state\": \"default\",\n  \"seeded\": {\n    \"organizations\": 7\n  }\n}\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

// The three commands whose body is required refuse the flags path when the
// field flag is unset, before the request fires.
func TestRequiredFieldFlags_AreRefusedWhenUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"schema steps": {[]string{"schema", "steps"}, "--steps is required unless --body is given"},
		"schema force": {[]string{"schema", "force"}, "--version is required unless --body is given"},
		"state":        {[]string{"state"}, "--state is required unless --body is given"},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestBody_IsExclusiveWithTheFieldFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		flag string
	}{
		"schema down":  {[]string{"schema", "down", "--body", `{}`, "--steps", "1"}, "steps"},
		"schema steps": {[]string{"schema", "steps", "--body", `{}`, "--steps", "1"}, "steps"},
		"schema force": {[]string{"schema", "force", "--body", `{}`, "--version", "1"}, "version"},
		"seed":         {[]string{"seed", "--body", `{}`, "--state", "x"}, "state"},
		"state":        {[]string{"state", "--body", `{}`, "--state", "x"}, "state"},
	} {
		s, srv := newService(t, http.StatusOK, `{}`)
		_, err := run(t, srv, tc.args...)
		if err == nil || !strings.Contains(err.Error(), "if any flags in the group [body "+tc.flag+"] are set none of the others can be") {
			t.Errorf("%s: err = %v, want cobra's mutual exclusion error", name, err)
		}
		if len(s.received) != 0 {
			t.Errorf("%s: the request fired anyway", name)
		}
	}
}

func TestCommands_ReturnTheProblemAnErrorStatusCarries(t *testing.T) {
	_, srv := newService(t, http.StatusConflict, `{"type":"about:blank","title":"Conflict","status":409,"detail":"dirty at version 2"}`)
	_, err := run(t, srv, "schema", "up")
	var p web.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v (%T), want a web.Problem", err, err)
	}
	if p.Status != 409 || p.Detail != "dirty at version 2" {
		t.Errorf("problem = %+v", p)
	}
}

func TestCommands_ExpectOK(t *testing.T) {
	_, srv := newService(t, http.StatusNoContent, "")
	_, err := run(t, srv, "seed")
	if err == nil || !strings.Contains(err.Error(), "want 200") {
		t.Fatalf("err = %v, want the status mismatch", err)
	}
}
