package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/internal/termtest"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// execute runs the app with args in place of the process's own and returns
// what it wrote to stdout and stderr and the exit code.
func execute(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out bytes.Buffer
	errOut, code := executeTo(t, &out, args...)
	return out.String(), errOut, code
}

// executeTo is execute with stdout supplied by the caller, for a test that
// needs it to be something other than a buffer. It returns what the app
// wrote to stderr and the exit code.
func executeTo(t *testing.T, stdout io.Writer, args ...string) (stderr string, code int) {
	t.Helper()
	var errOut bytes.Buffer
	a := New(stdout, &errOut)
	a.root.SetArgs(args)
	code = a.Run(context.Background())
	return errOut.String(), code
}

// exchange is one request as the fake service received it.
type exchange struct {
	method, uri string
}

// newService starts a fake service that answers every request with status
// and reply and records what it received. An error status is answered as a
// problem document.
func newService(t *testing.T, status int, reply string) (*[]exchange, *httptest.Server) {
	t.Helper()
	var received []exchange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, exchange{method: r.Method, uri: r.URL.RequestURI()})
		if status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", web.ProblemMediaType)
		} else {
			w.Header().Set("Content-Type", web.JSONMediaType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return &received, srv
}

func TestRoot_PrintsHelpAndTheListing(t *testing.T) {
	out, errOut, code := execute(t)
	if code != 0 {
		t.Fatalf("root exited %d: %s", code, errOut)
	}
	for _, want := range []string{"Available Commands:", "org", "admin", "demo", "list", "Scenarios:", "  sqlate    ", "  domain    ", "  problems  "} {
		if !strings.Contains(out, want) {
			t.Errorf("root output lacks %q:\n%s", want, out)
		}
	}
}

func TestList_PrintsTheScenariosInPresentationOrder(t *testing.T) {
	out, errOut, code := execute(t, "list")
	if code != 0 {
		t.Fatalf("list exited %d: %s", code, errOut)
	}
	sqlate, domain, problems := strings.Index(out, "  sqlate    "), strings.Index(out, "  domain    "), strings.Index(out, "  problems  ")
	if sqlate < 0 || domain < sqlate || problems < domain {
		t.Errorf("list does not print sqlate, domain, problems in that order:\n%s", out)
	}
}

func TestDemo_MountsEachScenario(t *testing.T) {
	out, errOut, code := execute(t, "demo")
	if code != 0 {
		t.Fatalf("demo exited %d: %s", code, errOut)
	}
	for _, want := range []string{"Available Commands:", "sqlate", "domain", "problems"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo help lacks %q:\n%s", want, out)
		}
	}
	if _, errOut, code := execute(t, "demo", "none"); code == 0 || errOut == "" {
		t.Errorf("demo none exited %d with stderr %q; want a failure that says why", code, errOut)
	}
}

func TestOrg_MountsTheOrganizationCommands(t *testing.T) {
	out, errOut, code := execute(t, "org")
	if code != 0 {
		t.Fatalf("org exited %d: %s", code, errOut)
	}
	for _, want := range []string{"Available Commands:", "create", "delete", "edit", "get", "get-by-path", "list", "transfer"} {
		if !strings.Contains(out, want) {
			t.Errorf("org help lacks %q:\n%s", want, out)
		}
	}
}

func TestAdmin_MountsTheDatabaseCommands(t *testing.T) {
	out, errOut, code := execute(t, "admin")
	if code != 0 {
		t.Fatalf("admin exited %d: %s", code, errOut)
	}
	if !strings.Contains(out, "database") {
		t.Errorf("admin help lacks database:\n%s", out)
	}
	out, errOut, code = execute(t, "admin", "database")
	if code != 0 {
		t.Fatalf("admin database exited %d: %s", code, errOut)
	}
	for _, want := range []string{"schema", "seed", "state", "diagnostics", "patterns", "statements", "states"} {
		if !strings.Contains(out, want) {
			t.Errorf("admin database help lacks %q:\n%s", want, out)
		}
	}
	out, errOut, code = execute(t, "admin", "database", "schema")
	if code != 0 {
		t.Fatalf("admin database schema exited %d: %s", code, errOut)
	}
	for _, want := range []string{"status", "verify", "up", "down", "steps", "force"} {
		if !strings.Contains(out, want) {
			t.Errorf("admin database schema help lacks %q:\n%s", want, out)
		}
	}
}

// The base URL is a persistent flag parsed during execution, after New has
// built the tree. A request reaching the server named by --base proves the
// client was constructed after parsing, from the parsed value, not at
// construction from the default.
func TestCommands_BindTheClientToTheBaseFlagAsParsed(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		method string
		uri    string
	}{
		"org": {
			args:   []string{"org", "get", "1"},
			method: http.MethodGet,
			uri:    "/api/organizations/1",
		},
		"admin database": {
			args:   []string{"admin", "database", "diagnostics"},
			method: http.MethodGet,
			uri:    "/admin/database/diagnostics",
		},
	} {
		t.Run(name, func(t *testing.T) {
			received, srv := newService(t, http.StatusOK, `{"ok":true}`)
			out, errOut, code := execute(t, append(tc.args, "--base", srv.URL)...)
			if code != 0 {
				t.Fatalf("exited %d: %s", code, errOut)
			}
			if len(*received) != 1 || (*received)[0] != (exchange{tc.method, tc.uri}) {
				t.Errorf("the service received %+v, want one %s %s", *received, tc.method, tc.uri)
			}
			if !strings.Contains(out, `"ok": true`) {
				t.Errorf("stdout lacks the rendered body:\n%s", out)
			}
		})
	}
}

// --no-color is a persistent flag parsed during execution, after New has
// built the tree and the output the commands render through. Color on a
// terminal without the flag, and none with it, proves the output reads the
// flag when a command renders, from the parsed value, not at construction
// from the default. The terminal is the test's own: under go test the
// process's stdout is never one, so a color decision taken there would be
// off in both cases and tell the two apart from nothing.
func TestCommands_ColorTheResultAsTheNoColorFlagSays(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	on := style.New(true)
	colored := on.Key(`"code"`) + ": " + on.Value(`"acme"`)
	for name, tc := range map[string]struct {
		args    []string
		colored bool
	}{
		"org":                       {[]string{"org", "get", "1"}, true},
		"org --no-color":            {[]string{"org", "get", "1", "--no-color"}, false},
		"admin database":            {[]string{"admin", "database", "diagnostics"}, true},
		"admin database --no-color": {[]string{"admin", "database", "diagnostics", "--no-color"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, srv := newService(t, http.StatusOK, `{"code":"acme"}`)
			term := termtest.Open(t)
			errOut, code := executeTo(t, term.Writer(), append(tc.args, "--base", srv.URL)...)
			if code != 0 {
				t.Fatalf("exited %d: %s", code, errOut)
			}
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

// The same run through a buffer, which is no terminal, is never colored
// whatever the flag says: the decision is about the stream written to.
func TestCommands_NeverColorAStreamThatIsNoTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	_, srv := newService(t, http.StatusOK, `{"code":"acme"}`)
	out, errOut, code := execute(t, "org", "get", "1", "--base", srv.URL)
	if code != 0 {
		t.Fatalf("exited %d: %s", code, errOut)
	}
	if want := "{\n  \"code\": \"acme\"\n}\n"; out != want {
		t.Errorf("stdout = %q, want the plain body %q", out, want)
	}
}

func TestConfig_FallsBackToSLABBase(t *testing.T) {
	received, srv := newService(t, http.StatusOK, `{}`)
	t.Setenv("SLAB_BASE", srv.URL)
	_, errOut, code := execute(t, "org", "get", "1")
	if code != 0 {
		t.Fatalf("exited %d: %s", code, errOut)
	}
	if len(*received) != 1 {
		t.Errorf("the service received %+v, want one request", *received)
	}
}

func TestRun_RendersTheErrorAndExitsNonZero(t *testing.T) {
	t.Run("a problem document, member by member", func(t *testing.T) {
		_, srv := newService(t, http.StatusNotFound, `{"status":404,"title":"Not Found","detail":"no organization 1","instance":"/api/organizations/1"}`)
		out, errOut, code := execute(t, "org", "get", "1", "--base", srv.URL)
		if code != 1 {
			t.Errorf("exited %d, want 1", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want nothing", out)
		}
		if want := "404 Not Found\ndetail: no organization 1\ninstance: /api/organizations/1\n"; errOut != want {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	})
	t.Run("a usage error, as its message", func(t *testing.T) {
		_, errOut, code := execute(t, "org", "get")
		if code != 1 {
			t.Errorf("exited %d, want 1", code)
		}
		if !strings.Contains(errOut, "accepts 1 arg(s), received 0") {
			t.Errorf("stderr = %q, want cobra's argument count message", errOut)
		}
	})
}
