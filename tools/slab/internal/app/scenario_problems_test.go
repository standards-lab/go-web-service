package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

const absentID = "00000000-0000-7000-8000-000000000000"

func runProblems(t *testing.T) (*orgFake, string) {
	t.Helper()
	fake := newOrgFake()
	r := runOrgScenario(t, "problems", fake, func() error { return fake.err })
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	if want := []string{"service", "base", "client", "grafana up", "grafana", "repo", "no color", "lifecycle config"}; !slices.Equal(r.built, want) {
		t.Errorf("nodes built = %q, want %q", r.built, want)
	}
	return fake, r.stdout
}

func TestScenario_ProblemsRunsTwelveStepsInOrder(t *testing.T) {
	_, out := runProblems(t)
	inOrder(t, out, []string{
		"[1/12] Initialization",
		"[2/12] Malformed Identifier", "[3/12] Malformed Query", "[4/12] Malformed Body",
		"[5/12] Oversized Body", "[6/12] Domain Validation", "[7/12] Missing If-Match",
		"[8/12] Malformed If-Match", "[9/12] Stale Version", "[10/12] Not Found",
		"[11/12] Duplicate Code", "[12/12] Transfer Cycle",
	})
}

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

func TestScenario_ProblemsNarratesEachConditionAsNoteRequestResponseTrace(t *testing.T) {
	_, out := runProblems(t)
	for _, c := range problemSections {
		text := step(t, out, c.heading)
		unwrapped := strings.Join(strings.Fields(text), " ")
		note, firstRequestLine := strings.Index(unwrapped, c.note), strings.Index(unwrapped, c.request+" ")
		status, trace := strings.Index(unwrapped, c.status), strings.Index(unwrapped, "Observability Grafana : ")
		if note < 0 || firstRequestLine < 0 || status < 0 || trace < 0 || !strings.Contains(unwrapped, "Trace ID : "+fakeTrace) {
			t.Errorf("step %s lacks its note %q, request %q, status %q, or trace pointer:\n%s", c.heading, c.note, c.request, c.status, text)
			continue
		}
		if note > firstRequestLine || firstRequestLine > status || status > trace {
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

func TestScenario_ProblemsShowsTheOversizedBodyAsItsStandIn(t *testing.T) {
	fake, out := runProblems(t)
	const indentedJSONStandIn = `"code": "<65537 bytes>"`
	if !strings.Contains(step(t, out, "[5/12]"), indentedJSONStandIn) {
		t.Errorf("the oversized body's step lacks the stand-in %s:\n%s", indentedJSONStandIn, out)
	}
	if strings.Contains(out, strings.Repeat("a", 64)) {
		t.Errorf("output shows the filler itself:\n%s", out)
	}
	if !slices.Contains(fake.requests, "POST /api/organizations") {
		t.Errorf("the fake did not receive the oversized create; got:\n%s", strings.Join(fake.requests, "\n"))
	}
}

func TestScenario_ProblemsSendsEachConditionsRequestAsNarrated(t *testing.T) {
	fake, _ := runProblems(t)
	const startProbes, initialization, conditions = 2, 2, 11
	if got := len(fake.requests); got != startProbes+initialization+conditions {
		t.Errorf("the fake saw %d requests, want the 2 start probes, Initialization's reset and list, and one per condition, 15 in all:\n%s", got, strings.Join(fake.requests, "\n"))
	}
	acme := fakeID(1)
	for _, want := range []string{
		"GET /readyz",
		"GET /api/health",
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
		if !slices.Contains(fake.requests, want) {
			t.Errorf("the fake did not receive %q; got:\n%s", want, strings.Join(fake.requests, "\n"))
		}
	}
}

func TestScenario_ProblemsWritesNoRow(t *testing.T) {
	fake, _ := runProblems(t)
	if got := len(fake.rows); got != len(seededTree) {
		t.Fatalf("%d rows remain, want the seeded %d", got, len(seededTree))
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

func TestScenario_ProblemsFailsTheStepWhenTheStatusIsNotTheOneNarrated(t *testing.T) {
	fake := newOrgFake()
	minted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == organization.Organizations+"/"+absentID {
			w.Header().Set("X-Request-Id", fakeTrace)
			writeJSON(w, http.StatusOK, organization.Organization{ID: absentID, Code: "minted", Version: 1})
			return
		}
		fake.ServeHTTP(w, r)
	})
	r := runOrgScenario(t, "problems", minted, func() error { return fake.err })

	if r.code != process.ExitFailure {
		t.Errorf("code = %d, want %d", r.code, process.ExitFailure)
	}
	if want := "slab scenario problems: step 10 (Not Found): status = 200 OK, want 404"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if n := strings.Count(r.stderr, "slab scenario"); n != 1 {
		t.Errorf("stderr reports %d times, want once:\n%s", n, r.stderr)
	}
	notFound := step(t, r.stdout, "[10/12]")
	if !strings.Contains(notFound, "HTTP 200 OK") {
		t.Errorf("the step does not show the response it rejected:\n%s", notFound)
	}
	if strings.Contains(notFound, "Trace ID") {
		t.Errorf("the step showed a trace pointer for a response it rejected:\n%s", notFound)
	}
	if strings.Contains(r.stdout, "[11/12]") {
		t.Errorf("the run went on past the failed step:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "step 10") {
		t.Errorf("the failure is reported in the narration as well as on stderr:\n%s", r.stdout)
	}
}
