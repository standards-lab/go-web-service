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

// The fixtures the direct command families' tests share: a fake service,
// one run of the App over a family's command path against it, and the
// checks on what that run did.

// exchange is one request as the fake service received it.
type exchange struct {
	method, uri, ifMatch, contentType, body string
	contentLength                           int64
}

// service is a fake that answers every request with one canned reply,
// with location as its Location header when it is set, and records what
// it received.
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
			method:        r.Method,
			uri:           r.URL.RequestURI(),
			ifMatch:       r.Header.Get("If-Match"),
			contentType:   r.Header.Get("Content-Type"),
			contentLength: r.ContentLength,
			body:          string(body),
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

// result is what one run of the App over a family's command path did.
type result struct {
	path           string // the family's command path, such as "admin database"
	stdout, stderr string
	code           int
	built          []string // the nodes the run built, in order
}

// familyBuilt is what every direct command builds: the no color node its
// output source names and the client node it declares, with the base node
// the client is bound to, and the lifecycle configuration the dispatcher
// adds.
var familyBuilt = []string{"no color", "client", "base", "lifecycle config"}

// runFamily runs a fresh App over path and args, with --base naming srv,
// and returns what it did. A nil stdout is a buffer whose content is
// result.stdout; a stdout the caller supplies, for a test that needs it to
// be a terminal, leaves result.stdout empty.
func runFamily(t *testing.T, stdout io.Writer, path []string, srv *httptest.Server, args ...string) result {
	t.Helper()
	if stdout == nil {
		stdout = new(bytes.Buffer)
	}
	var stderr bytes.Buffer
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &stderr})
	rec := apptest.Builds(a)
	code := a.Run(context.Background(), slices.Concat(path, args, []string{"--base", srv.URL}))
	r := result{path: strings.Join(path, " "), stderr: stderr.String(), code: code, built: rec.Log()}
	if b, ok := stdout.(*bytes.Buffer); ok {
		r.stdout = b.String()
	}
	return r
}

// ok fails the test unless r succeeded, having built what a direct command
// declares.
func (r result) ok(t *testing.T) {
	t.Helper()
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", r.code, process.ExitOK, r.stderr)
	}
	if !slices.Equal(r.built, familyBuilt) {
		t.Errorf("nodes built = %q, want %q", r.built, familyBuilt)
	}
}

// usage fails the test unless r is a usage error reported with want and
// the command's usage, having built nothing and written nothing to stdout.
func (r result) usage(t *testing.T, want string) {
	t.Helper()
	if r.code != process.ExitUsage {
		t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitUsage, r.stderr)
	}
	if !strings.Contains(r.stderr, want) || !strings.Contains(r.stderr, "\nUsage: slab "+r.path+" ") {
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

// failed fails the test unless r failed with a report on stderr that
// starts with the command's path, the family's then path, and contains
// want.
func (r result) failed(t *testing.T, path, want string) {
	t.Helper()
	if r.code != process.ExitFailure || !strings.HasPrefix(r.stderr, "slab "+r.path+" "+path+": ") || !strings.Contains(r.stderr, want) {
		t.Errorf("code = %d, stderr = %q; want %d and %q", r.code, r.stderr, process.ExitFailure, want)
	}
}
