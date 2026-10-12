package app_test

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
)

// runStorage starts a fake service that answers every request with status
// and reply, runs a fresh App over "admin storage" and args against it,
// and returns the requests it received, what the run wrote, its exit code,
// and the nodes it built.
func runStorage(t *testing.T, status int, reply string, args ...string) (got []string, stdout, stderr string, code int, log []string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+string(body))
		if status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", web.ProblemMediaType)
		} else {
			w.Header().Set("Content-Type", web.JSONMediaType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	var out, errOut bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
	rec := apptest.Builds(a)
	code = a.Run(context.Background(), slices.Concat([]string{"admin", "storage"}, args, []string{"--base", srv.URL}))
	return got, out.String(), errOut.String(), code, rec.Log()
}

// Each subcommand sends its endpoint's request with no body, prints the
// reply, and builds only the nodes it declares.
func TestStorage_SendOneBodilessRequestPerEndpoint(t *testing.T) {
	for args, want := range map[string]string{
		"diagnostics": "GET /admin/storage/diagnostics",
		"container":   "POST /admin/storage/container",
	} {
		t.Run(args, func(t *testing.T) {
			got, stdout, stderr, code, log := runStorage(t, http.StatusOK, `{"ready":true}`, strings.Fields(args)...)
			if code != process.ExitOK {
				t.Fatalf("code = %d; stderr = %q", code, stderr)
			}
			if len(got) != 1 || got[0] != want {
				t.Errorf("requests = %v, want %q", got, want)
			}
			if want := "{\n  \"ready\": true\n}\n"; stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}
			if !slices.Equal(log, familyBuilt) {
				t.Errorf("nodes built = %q, want %q", log, familyBuilt)
			}
		})
	}
}

// A parent run with no subcommand prints its help, listing both leaves,
// and builds nothing.
func TestStorage_MountOneSubcommandPerEndpoint(t *testing.T) {
	got, stdout, _, code, log := runStorage(t, http.StatusOK, `{}`)
	if code != process.ExitUsage {
		t.Errorf("code = %d, want the help's %d", code, process.ExitUsage)
	}
	for _, want := range []string{"diagnostics", "container"} {
		if !strings.Contains(stdout, "\n  "+want+" ") {
			t.Errorf("storage help lacks %q:\n%s", want, stdout)
		}
	}
	if len(got) != 0 || len(log) != 0 {
		t.Errorf("requests = %v, nodes built = %q; want none", got, log)
	}
}

// An argument is a usage error: nothing is built and nothing is sent.
func TestStorage_RefuseArguments(t *testing.T) {
	for _, leaf := range []string{"diagnostics", "container"} {
		t.Run(leaf, func(t *testing.T) {
			got, stdout, stderr, code, log := runStorage(t, http.StatusOK, `{}`, leaf, "x")
			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d", code, process.ExitUsage)
			}
			if want := "slab admin storage " + leaf + ": accepts no arguments, got 1\nUsage: slab admin storage " + leaf; !strings.HasPrefix(stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, want)
			}
			if stdout != "" || len(got) != 0 || len(log) != 0 {
				t.Errorf("stdout = %q, requests = %v, nodes built = %q; want none", stdout, got, log)
			}
		})
	}
}

// A problem response is reported once on stderr, with nothing on stdout.
func TestStorage_ReportTheProblemAnErrorStatusCarries(t *testing.T) {
	_, stdout, stderr, code, _ := runStorage(t, http.StatusServiceUnavailable, `{"status":503,"title":"Service Unavailable","detail":"the store is unreachable"}`, "diagnostics")
	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if want := "slab admin storage diagnostics: 503 Service Unavailable\ndetail: the store is unreachable\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}
