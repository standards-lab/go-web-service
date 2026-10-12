package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

// scenarioRun is what one "slab scenario <name>" run left: its exit code,
// its narration and its report, and the nodes it built.
type scenarioRun struct {
	code           int
	stdout, stderr string
	built          []string
}

// runScenario runs "slab scenario <name>" over a fresh App with the
// service at base and Grafana at grafana, set through SLAB_BASE and
// SLAB_GRAFANA, from the working directory under the repository, so a
// step that reads the repository reads the real seed file and fixtures.
func runScenario(t *testing.T, name, base, grafana string) scenarioRun {
	t.Helper()
	t.Setenv("SLAB_BASE", base)
	t.Setenv("SLAB_GRAFANA", grafana)
	a, stdout, stderr, built := newApp(t)
	code := a.Run(context.Background(), []string{"scenario", name})
	return scenarioRun{code: code, stdout: stdout.String(), stderr: stderr.String(), built: built.Log()}
}

// runOrgScenario runs the scenario name against fake, serving as both the
// service and Grafana, and closes the fake's listener before returning, so
// the fake's state is read without its lock from then on. A request no
// route of the fake answers fails the test.
func runOrgScenario(t *testing.T, name string, fake http.Handler, failed func() error) scenarioRun {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	r := runScenario(t, name, srv.URL, srv.URL)
	srv.Close()
	if err := failed(); err != nil {
		t.Fatalf("the scenario sent a bad request: %v\n%s", err, r.stdout)
	}
	return r
}

// runDomain runs the domain scenario against a fresh orgFake, expecting it
// to succeed.
func runDomain(t *testing.T) (*orgFake, string) {
	t.Helper()
	fake := newOrgFake()
	r := runOrgScenario(t, "domain", fake, func() error { return fake.err })
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	return fake, r.stdout
}

// step returns the narration of one step of out: from its heading to the
// next heading.
func step(t *testing.T, out, heading string) string {
	t.Helper()
	start := strings.Index(out, heading)
	if start < 0 {
		t.Fatalf("output lacks %q:\n%s", heading, out)
	}
	rest := out[start+len(heading):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// inOrder fails t unless every heading appears in out, in order, and none
// past them, at [n+1/.
func inOrder(t *testing.T, out string, headings []string) {
	t.Helper()
	last := -1
	for _, h := range headings {
		at := strings.Index(out, h)
		if at < 0 {
			t.Fatalf("output lacks %q:\n%s", h, out)
		}
		if at < last {
			t.Errorf("%q is out of order", h)
		}
		last = at
	}
	if extra := "[" + strconv.Itoa(len(headings)+1) + "/"; strings.Contains(out, extra) {
		t.Errorf("the scenario narrates more than %d steps:\n%s", len(headings), out)
	}
}

// The domain scenario builds the need nodes it declares, service and
// grafana up, with the settings they read, then the settings its steps
// read, and runs its ten steps in order.
func TestScenario_DomainRunsTenStepsInOrder(t *testing.T) {
	fake := newOrgFake()
	r := runOrgScenario(t, "domain", fake, func() error { return fake.err })
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	inOrder(t, r.stdout, []string{
		"[1/10] Initialization", "[2/10] Raw List", "[3/10] Queried List", "[4/10] Find",
		"[5/10] Find by Path", "[6/10] Create", "[7/10] Edit", "[8/10] Transfer",
		"[9/10] Results", "[10/10] Delete",
	})
	if want := []string{"service", "base", "grafana up", "grafana", "repo", "no color", "lifecycle config"}; !slices.Equal(r.built, want) {
		t.Errorf("nodes built = %q, want %q", r.built, want)
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Errorf("narration to a stream that is no terminal carries ANSI escapes:\n%s", r.stdout)
	}
}

func TestScenario_DomainNarratesEachStepBeforeItsRequest(t *testing.T) {
	_, out := runDomain(t)
	for _, c := range []struct{ heading, note, request string }{
		{"[1/10]", `posts the "default" state`, "POST /admin/database/state"},
		{"[2/10]", "A list request with no query component", "GET /api/organizations\n"},
		{"[3/10]", "A filter is a query parameter named for a field", "GET /api/organizations?"},
		{"[6/10]", "This creates sales under acme", "POST /api/organizations\n"},
		{"[7/10]", "Rename the seeded finance row", "PUT /api/organizations/"},
		{"[8/10]", "A transfer is a structural move", "/transfer"},
		{"[9/10]", "Retrieving the raw list data again", "GET /api/organizations\n"},
		{"[10/10]", "Delete only the sales row", "DELETE /api/organizations/"},
	} {
		text := step(t, out, c.heading)
		note, request := strings.Index(text, c.note), strings.Index(text, c.request)
		if note < 0 || request < 0 {
			t.Errorf("step %s lacks %q or %q:\n%s", c.heading, c.note, c.request, text)
			continue
		}
		if note > request {
			t.Errorf("step %s: the note %q follows the request %q:\n%s", c.heading, c.note, c.request, text)
		}
	}
	// The create step's trace pointer follows its response, under a plain
	// caption, its lines a block past the prose.
	create := step(t, out, "[6/10]")
	response := strings.Index(create, "    HTTP 201 Created\n")
	trace := tracePointer.FindStringIndex(create)
	if response < 0 || trace == nil || trace[0] < response {
		t.Errorf("the trace block does not follow the create response:\n%s", create)
	}
}

// tracePointer is a trace pointer block as a step shows it: the plain
// Observability caption, then the Explore page under the fake's URL, the
// service name, and the fake's trace id, a block past the prose.
var tracePointer = regexp.MustCompile(`\n  Observability\n    Grafana     : http://127\.0\.0\.1:\d+/explore\n    Service Name: go-web-service\n    Trace ID    : ` + fakeTrace + `\n`)

// A request and its response each show as a block, set off from the prose
// and from each other by one blank line, every line of it past the prose.
func TestScenario_DomainShowsTheRequestAndResponseAsBlocks(t *testing.T) {
	_, out := runDomain(t)
	find := step(t, out, "[4/10] Find")
	want := "\n\n    GET /api/organizations/" + fakeID(1) + "\n\n    HTTP 200 OK\n\n    {\n      \"id\": \"" + fakeID(1) + "\","
	if !strings.HasPrefix(find, want) {
		t.Errorf("the find step does not open with its request and response blocks:\n%q\nwant it to start with\n%q", find, want)
	}
}

func TestScenario_DomainShowsTheSeedFileBeforeTheReset(t *testing.T) {
	_, out := runDomain(t)
	caption, reset := strings.Index(out, "  data/seeds/default.json\n"), strings.Index(out, "POST /admin/database/state")
	if caption < 0 || reset < 0 || caption > reset {
		t.Fatalf("the seed file does not precede the reset request:\n%s", out)
	}
	for _, want := range []string{`"organizations": [`, `"code": "acme"`, `"name": "Acme Corporation"`, `"parent": "operations"`} {
		if !strings.Contains(out[caption:reset], want) {
			t.Errorf("the seed block lacks %s:\n%s", want, out[caption:reset])
		}
	}
}

func TestScenario_DomainSendsTheQueryWithLiteralBrackets(t *testing.T) {
	fake, out := runDomain(t)
	const line = "GET /api/organizations?code[like]=%25o%25&size=2&page=1"
	if !strings.Contains(out, "    "+line+"\n") {
		t.Errorf("output lacks the request line %q:\n%s", line, out)
	}
	if !slices.Contains(fake.requests, line) {
		t.Errorf("the fake did not receive %q; got:\n%s", line, strings.Join(fake.requests, "\n"))
	}
	if strings.Contains(out, "%5B") || strings.Contains(out, "%5D") {
		t.Errorf("the brackets are percent-encoded:\n%s", out)
	}
}

func TestScenario_DomainTargetsThreeDifferentRowsEachAtItsOwnVersion(t *testing.T) {
	fake, out := runDomain(t)
	finance, logistics, engineering := fakeID(7), fakeID(6), fakeID(2)
	sales := fakeID(8)
	for _, want := range []string{
		"GET /readyz",
		"GET /api/health",
		"POST /admin/database/state",
		"GET /api/organizations",
		"GET /api/organizations/" + fakeID(1),
		"GET /api/organizations/lookup?path=/acme/engineering/platform",
		"POST /api/organizations",
		"PUT /api/organizations/" + finance + ` If-Match: "1"`,
		"POST /api/organizations/" + logistics + `/transfer If-Match: "1"`,
		"DELETE /api/organizations/" + sales + ` If-Match: "1"`,
	} {
		if !slices.Contains(fake.requests, want) {
			t.Errorf("the fake did not receive %q; got:\n%s", want, strings.Join(fake.requests, "\n"))
		}
	}
	for _, want := range []string{
		`"code": "finance"`, `"name": "Finance and Accounting"`,
		`"parent_id": "` + engineering + `"`,
		`"code": "sales"`, `"name": "Sales"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	if o := fake.byCode("finance"); o.Name != "Finance and Accounting" || o.Version != 2 {
		t.Errorf("finance = %+v, want renamed at version 2", o)
	}
	if o := fake.byCode("logistics"); o.ParentID == nil || *o.ParentID != engineering || o.Version != 2 {
		t.Errorf("logistics = %+v, want under engineering at version 2", o)
	}
	if fake.byCode("sales") != nil {
		t.Error("sales survived the delete")
	}
	if got := len(fake.rows); got != 7 {
		t.Errorf("%d rows remain, want the seeded 7", got)
	}
}
