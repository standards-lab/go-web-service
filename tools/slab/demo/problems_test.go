package demo

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// runProblems runs the problems scenario against the fake.
func runProblems(t *testing.T) (*fakeService, string) {
	t.Helper()
	return runScenario(t, Problems())
}

func TestProblems_RunsTwelveStepsInOrderAgainstTheFake(t *testing.T) {
	_, out := runProblems(t)
	steps := []string{
		"[1/12] Initialization",
		"[2/12] Malformed Identifier", "[3/12] Malformed Query", "[4/12] Malformed Body",
		"[5/12] Oversized Body", "[6/12] Domain Validation", "[7/12] Missing If-Match",
		"[8/12] Malformed If-Match", "[9/12] Stale Version", "[10/12] Not Found",
		"[11/12] Duplicate Code", "[12/12] Transfer Cycle",
	}
	last := -1
	for _, step := range steps {
		at := strings.Index(out, step)
		if at < 0 {
			t.Fatalf("output lacks %q:\n%s", step, out)
		}
		if at < last {
			t.Errorf("%q is out of order", step)
		}
		last = at
	}
	if strings.Contains(out, "[13/") {
		t.Errorf("the scenario narrates more than twelve steps:\n%s", out)
	}
}

// problemSections is each condition's step: its heading, a phrase from its
// note, the request line it sends, the status line it expects, and the
// problem detail the fake writes with the real service's wording (empty
// where the status carries none).
var problemSections = []struct {
	heading, note, request, status, detail string
}{
	{"[2/12]", "is first parsed as a UUID", "GET /api/organizations/not-a-uuid", "HTTP 400 Bad Request", `path id=\"not-a-uuid\": must be a UUID`},
	{"[3/12]", "page=0 breaks the first rule", "GET /api/organizations?page=0", "HTTP 400 Bad Request", `query page=\"0\": must be an integer of at least 1`},
	{"[4/12]", "stops before its closing brace", "POST /api/organizations", "HTTP 400 Bad Request", "body: unexpected EOF"},
	{"[5/12]", "bounds a command body at 65536 bytes", "POST /api/organizations", "HTTP 413 Request Entity Too Large", "body: exceeds the 65536-byte limit"},
	{"[6/12]", "mirror the schema's check constraints", "POST /api/organizations", "HTTP 400 Bad Request", "invalid command: code must be lowercase words joined by single hyphens"},
	{"[7/12]", "no If-Match results in a 428", "PUT /api/organizations/" + fakeID(1), "HTTP 428 Precondition Required", "the request requires an If-Match header"},
	{"[8/12]", "A weak tag", "PUT /api/organizations/" + fakeID(1), "HTTP 400 Bad Request", `If-Match \"W/\\\"1\\\"\": must be one entity-tag`},
	{"[9/12]", "out of date version", "PUT /api/organizations/" + fakeID(1), "HTTP 412 Precondition Failed", ""},
	{"[10/12]", "results in a 404 response", "GET /api/organizations/" + absentID, "HTTP 404 Not Found", ""},
	{"[11/12]", "collides with the seeded", "POST /api/organizations", "HTTP 409 Conflict", ""},
	{"[12/12]", "the cycle check identifies and refuses", "POST /api/organizations/" + fakeID(1) + "/transfer", "HTTP 409 Conflict", ""},
}

func TestProblems_NarratesEachConditionAsNoteRequestResponseTrace(t *testing.T) {
	_, out := runProblems(t)
	for _, c := range problemSections {
		text := step(t, out, c.heading)
		// The note wraps at 80 columns, so its phrase is sought with the
		// step's whitespace collapsed; the request line is the first place
		// its text appears, ahead of the problem's instance member.
		flat := strings.Join(strings.Fields(text), " ")
		note, request := strings.Index(flat, c.note), strings.Index(flat, c.request+" ")
		status, trace := strings.Index(flat, c.status), strings.Index(flat, "Trace ID : "+fakeTrace)
		if note < 0 || request < 0 || status < 0 || trace < 0 {
			t.Errorf("step %s lacks its note %q, request %q, status %q, or trace pointer:\n%s", c.heading, c.note, c.request, c.status, text)
			continue
		}
		if note > request || request > status || status > trace {
			t.Errorf("step %s does not run note, request, response, trace in that order:\n%s", c.heading, text)
		}
		for _, want := range []string{`"type": "about:blank"`, `"status": ` + c.status[5:8], `"request_id": "` + fakeTrace + `"`, "Content-Type: application/problem+json", "X-Request-Id: " + fakeTrace} {
			if !strings.Contains(text, want) {
				t.Errorf("step %s's response lacks %s:\n%s", c.heading, want, text)
			}
		}
		if c.detail == "" {
			if strings.Contains(text, `"detail"`) {
				t.Errorf("step %s's problem carries a detail it should not:\n%s", c.heading, text)
			}
		} else if !strings.Contains(text, `"detail": "`+c.detail) {
			t.Errorf("step %s's problem lacks the detail %q:\n%s", c.heading, c.detail, text)
		}
	}
}

func TestProblems_PrintsTheOversizedBodyAsItsStandIn(t *testing.T) {
	fake, out := runProblems(t)
	// The stand-in is JSON, so the request block prints it indented.
	const standIn = `"code": "<65537 bytes>"`
	if !strings.Contains(out, standIn) {
		t.Errorf("output lacks the stand-in %s:\n%s", standIn, out)
	}
	if strings.Contains(out, strings.Repeat("a", 64)) {
		t.Errorf("output prints the filler itself:\n%s", out)
	}
	if !contains(fake.requests, "POST /api/organizations") {
		t.Errorf("the fake did not receive the oversized create; got:\n%s", strings.Join(fake.requests, "\n"))
	}
}

func TestProblems_SendsEachConditionsRequestAsNarrated(t *testing.T) {
	fake, _ := runProblems(t)
	// The two need probes, the reset and list Initialization makes, then one
	// request per condition.
	if got := len(fake.requests); got != 2+13 {
		t.Errorf("the fake saw %d requests, want 15:\n%s", got, strings.Join(fake.requests, "\n"))
	}
	acme := fakeID(1)
	for _, want := range []string{
		"POST /admin/database/state",
		"GET /api/organizations",
		"GET /api/organizations/not-a-uuid",
		"GET /api/organizations?page=0",
		"POST /api/organizations",
		"PUT /api/organizations/" + acme,
		"PUT /api/organizations/" + acme + ` If-Match: W/"1"`,
		"PUT /api/organizations/" + acme + ` If-Match: "2"`,
		"GET /api/organizations/" + absentID,
		"POST /api/organizations/" + acme + `/transfer If-Match: "1"`,
	} {
		if !contains(fake.requests, want) {
			t.Errorf("the fake did not receive %q; got:\n%s", want, strings.Join(fake.requests, "\n"))
		}
	}
}

func TestProblems_WritesNoRow(t *testing.T) {
	fake, _ := runProblems(t)
	if got := len(fake.rows); got != len(seededTree) {
		t.Fatalf("%d rows remain, want the seeded %d", got, len(seededTree))
	}
	acme := fake.byCode("acme")
	if acme == nil || acme.ID != fakeID(1) || acme.ParentID != nil || acme.Name != "Acme Corporation" || acme.Version != 1 {
		t.Errorf("acme = %+v, want the seeded root at version 1", acme)
	}
	for i, row := range seededTree {
		o := fake.byCode(row[1])
		if o == nil || o.ID != fakeID(i+1) || o.Name != row[2] || o.Version != 1 {
			t.Errorf("%s = %+v, want the seeded row at version 1", row[1], o)
			continue
		}
		if parent := fake.parentOf(o); (row[0] == "") != (parent == nil) || (parent != nil && parent.Code != row[0]) {
			t.Errorf("%s is not under %q", row[1], row[0])
		}
	}
}

func TestProblems_KeepsEveryNoteLineWithinEightyColumns(t *testing.T) {
	_, out := runProblems(t)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && len(line) > 80 {
			t.Errorf("line is %d columns: %q", len(line), line)
		}
	}
}

// An expected problem status is the step's success; a status the step did
// not narrate, a success included, is the step's failure.
func TestProblems_FailsTheStepWhenTheStatusIsNotTheOneNarrated(t *testing.T) {
	srv := httptest.NewServer(newFakeService())
	t.Cleanup(srv.Close)
	ctx := env.WithContext(context.Background(), env.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
	s := &problemState{client: httpx.NewClient(srv.URL)}
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	res, err := s.client.Get(ctx, organization.Organizations+"/"+fakeID(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.observe(ctx, r, res, http.StatusNotFound); err == nil || !strings.Contains(err.Error(), "status = 200 OK, want 404") {
		t.Errorf("observe accepted a 200 where the step narrates a 404: %v", err)
	}
	if strings.Contains(out.String(), "Trace ID") {
		t.Errorf("observe printed a trace pointer for a response it rejected:\n%s", out.String())
	}
}

func TestList_ShowsTheProblemsScenarioWithItsFourNeeds(t *testing.T) {
	var out bytes.Buffer
	scenario.WriteListing(&out, Scenarios())
	listing := out.String()
	at := strings.Index(listing, "problems  The service's problem-response contract (RFC 9457)")
	if at < 0 {
		t.Fatalf("list lacks the problems scenario:\n%s", listing)
	}
	rest := listing[at:]
	for _, want := range []string{
		"needs postgres (mise run db-up)",
		"needs the observability profile (mise run otel-up)",
		"needs the service (mise run serve)",
		"needs Grafana (mise run otel-up)",
	} {
		if !strings.Contains(rest, want) {
			t.Errorf("list lacks %q under problems:\n%s", want, listing)
		}
	}
}
