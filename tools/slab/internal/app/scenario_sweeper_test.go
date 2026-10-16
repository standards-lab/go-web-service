package app_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process"
)

// mimirProxy is the Grafana route the sweeper scenario's queries reach
// Mimir through: the datasource proxy to the provisioned Mimir datasource.
const mimirProxy = "/api/datasources/proxy/uid/mimir/api/v1/query"

// passes is a fake export's sweep_passes_total: a key naming a reclaimer
// alone counts its drained passes, and "reclaimer/outcome" another
// outcome's. The scenario reads an outcome with no key as 0, as Mimir
// holds no series for an outcome that never happened.
type passes map[string]int

// sweeperFake stands in for the service and for Grafana with Mimir behind
// its datasource proxy, on one listener, for the sweeper scenario. The
// service is an eventsFake, so the branch's create, upload, and recursive
// delete are answered as the service answers them and its sweep runs after
// sweepReads reads of the marked directory. Mimir holds one export when
// the run starts, before; the first query after the fake's sweep has run
// publishes a second, after, sampled then, as the service's next export
// would carry the nudged pass. Each query's PromQL is recorded, and a
// query the fake does not recognize fails the test through err.
type sweeperFake struct {
	*eventsFake
	before, after passes
	exports       []fakeExport
	queries       []string
	// backlog is sweep_backlogged_ratio by reclaimer; a reclaimer it does
	// not name reports 0.
	backlog map[string]int
}

// fakeExport is one export as Mimir holds it: when it was sampled and the
// counts it carried.
type fakeExport struct {
	at     time.Time
	counts passes
}

func newSweeperFake(before, after passes) *sweeperFake {
	return &sweeperFake{
		eventsFake: newEventsFake(),
		before:     before,
		after:      after,
		exports:    []fakeExport{{at: time.Now().Add(-30 * time.Second), counts: before}},
	}
}

func (f *sweeperFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/health":
		w.WriteHeader(http.StatusOK)
	case mimirProxy:
		f.query(w, r)
	default:
		f.eventsFake.ServeHTTP(w, r)
	}
}

// swept reports whether the scenario's recursive delete was answered and
// its sweep has run: the fake recorded the delete's event and holds no
// marked directory.
func (f *sweeperFake) swept() bool {
	return slices.Contains(f.emitted, "web-service.document.directory.deleted") && len(f.deleting) == 0
}

// query answers one instant query from the latest export, in the
// Prometheus API's vector shape, publishing the after export first once
// the sweep has run.
func (f *sweeperFake) query(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query().Get("query")
	f.queries = append(f.queries, q)
	if f.swept() && len(f.exports) == 1 {
		// Sampled a second after now: the wire carries milliseconds, and
		// the scenario waits for a sample strictly after its sweep, which
		// a sample at now could truncate to before.
		f.exports = append(f.exports, fakeExport{at: time.Now().Add(time.Second), counts: f.after})
	}
	latest := f.exports[len(f.exports)-1]
	type entry struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	now := float64(time.Now().UnixMilli()) / 1000
	sample := func(labels map[string]string, v float64) entry {
		return entry{Metric: labels, Value: [2]any{now, fmt.Sprintf("%g", v)}}
	}
	var result []entry
	switch {
	case !strings.Contains(q, `job="go-web-service"`):
		f.err = fmt.Errorf("the query %q does not select the service's job", q)
	case strings.Contains(q, "timestamp(sweep_passes_total"):
		result = append(result, sample(map[string]string{}, float64(latest.at.UnixMilli())/1000))
	case strings.Contains(q, "sweep_backlogged_ratio"):
		for _, rec := range []string{"files", "outbox", "inbox"} {
			result = append(result, sample(map[string]string{"sweep_reclaimer": rec}, float64(f.backlog[rec])))
		}
	case strings.Contains(q, "sweep_passes_total"):
		for key, n := range latest.counts {
			rec, out, ok := strings.Cut(key, "/")
			if !ok {
				out = "drained"
			}
			result = append(result, sample(map[string]string{"sweep_reclaimer": rec, "sweep_outcome": out}, float64(n)))
		}
	default:
		f.err = fmt.Errorf("no answer for the query %q", q)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "vector", "result": result},
	})
}

// runSweeperScenario runs the sweeper scenario against fake, serving as
// both the service and Grafana, and returns the run.
func runSweeperScenario(t *testing.T, fake *sweeperFake) scenarioRun {
	t.Helper()
	return runOrgScenario(t, "sweeper", fake, func() error { return fake.err })
}

// runSweeperOK is runSweeperScenario expecting the run to succeed, and
// returns its narration.
func runSweeperOK(t *testing.T, fake *sweeperFake) string {
	t.Helper()
	r := runSweeperScenario(t, fake)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	return r.stdout
}

// joined returns out with its wrapped lines joined, so a test matches a
// sentence wherever the reporter wrapped it.
func joined(out string) string {
	return strings.Join(strings.Fields(out), " ")
}

// sweeperSteps are the sweeper scenario's headings, in order.
var sweeperSteps = []string{
	"[1/6] Pass Counts",
	"[2/6] Branch Creation",
	"[3/6] Recursive Delete",
	"[4/6] Next Export",
	"[5/6] Outbox Retention",
	"[6/6] Grafana View",
}

// The scenario runs its six steps in order, against a window where files
// ran one pass more than the interval gave outbox and inbox.
func TestScenario_SweeperRunsSixStepsInOrder(t *testing.T) {
	fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, passes{"files": 7, "outbox": 6, "inbox": 6})
	inOrder(t, runSweeperOK(t, fake), sweeperSteps)
}

// The first step shows the sweep block as config.local.json merges over
// config.json, each value naming its file, and the counts of the export
// it read, one row per reclaimer in the sweeper's order: each outcome under
// its own column, and the backlog gauge.
func TestScenario_SweeperShowsTheSweepBlockAndTheCounts(t *testing.T) {
	fake := newSweeperFake(
		passes{"files": 5, "outbox": 4, "outbox/more": 2, "inbox": 3, "inbox/error": 1},
		passes{"files": 6, "outbox": 4, "outbox/more": 2, "inbox": 3, "inbox/error": 1})
	fake.backlog = map[string]int{"outbox": 1}
	first := step(t, runSweeperOK(t, fake), sweeperSteps[0])
	for _, want := range []string{
		"interval    30s    (config.json)",
		"outbox_age  2m     (config.local.json)",
		"inbox_age   2m     (config.local.json)",
		"files   drained 5      more 0      error 0      backlogged 0",
		"outbox  drained 4      more 2      error 0      backlogged 1",
		"inbox   drained 3      more 0      error 1      backlogged 0",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("step 1 lacks %q:\n%s", want, first)
		}
	}
}

// The export after the sweep shows each reclaimer's passes in the window,
// and the narration says what the movement shows as it fell: files' extra
// pass over the interval's is the nudge's; outbox and inbox moving at all
// is their interval; and when files ran no more than they did, the counts
// do not single the nudged pass out.
func TestScenario_SweeperNarratesTheWindowAsItFell(t *testing.T) {
	for name, tc := range map[string]struct {
		after passes
		rows  []string
		want  []string
	}{
		"the nudge over a tick": {
			passes{"files": 7, "outbox": 6, "inbox": 6},
			[]string{"files   +2   drained 5→7", "outbox  +1   drained 5→6", "inbox   +1   drained 5→6"},
			[]string{"files ran 2 passes, the nudged one among them", "outbox ran 1 pass and inbox 1 pass: no command names them, so they ran on their 30s interval", "files' 1 pass over the most either of them ran is the nudge's"},
		},
		"no tick in the window": {
			passes{"files": 6, "outbox": 5, "inbox": 5},
			[]string{"files   +1   drained 5→6", "outbox  +0   drained 5 ", "inbox   +0   drained 5 "},
			[]string{"files ran 1 pass, the nudged one among them", "outbox and inbox ran none: no command names them, and their 30s interval did not come round in the window", "files' 1 pass over the most either of them ran is the nudge's"},
		},
		"a tick for all three": {
			passes{"files": 6, "outbox": 6, "inbox": 6},
			[]string{"files   +1   drained 5→6", "outbox  +1   drained 5→6", "inbox   +1   drained 5→6"},
			[]string{"outbox ran 1 pass and inbox 1 pass", "files ran no more than they did, so the counts alone do not single out the nudged pass in this window"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, tc.after)
			fourth := step(t, runSweeperOK(t, fake), sweeperSteps[3])
			for _, want := range tc.rows {
				if !strings.Contains(fourth, want) {
					t.Errorf("step 4's table lacks %q:\n%s", want, fourth)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(joined(fourth), want) {
					t.Errorf("step 4 does not say %q:\n%s", want, fourth)
				}
			}
		})
	}
}

// An export after the sweep that counts files no higher than before is
// missing the nudged pass: the run fails at Next Export, naming it, and
// goes no further.
func TestScenario_SweeperFailsWhenTheNudgedPassIsMissing(t *testing.T) {
	fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, passes{"files": 5, "outbox": 6, "inbox": 6})
	r := runSweeperScenario(t, fake)
	if r.code != process.ExitFailure {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitFailure, r.stderr, r.stdout)
	}
	if want := "slab scenario sweeper: step 4 (Next Export): the export at "; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if !strings.Contains(r.stderr, "counts files at 5 passes, as the first step's did: the nudged pass is missing") {
		t.Errorf("stderr = %q, want it to name the missing pass", r.stderr)
	}
	if strings.Contains(r.stdout, sweeperSteps[4]) {
		t.Errorf("the run went on past the failed step:\n%s", r.stdout)
	}
}

// The branch the run creates is named for the run and removed by its
// recursive delete, waited out, so the run leaves no document but the
// root its first write created, and a second run against the same fake
// succeeds too.
func TestScenario_SweeperLeavesNothingAndRunsTwice(t *testing.T) {
	fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, passes{"files": 6, "outbox": 5, "inbox": 5})
	out := runSweeperOK(t, fake)
	if !strings.Contains(step(t, out, sweeperSteps[2]), "The sweep removed the branch within ") {
		t.Errorf("step 3 does not report the wait:\n%s", out)
	}
	for _, n := range fake.nodes {
		if n.name != "/" {
			t.Errorf("the run left the document entry %q", n.name)
		}
	}
	second := newSweeperFake(passes{"files": 6, "outbox": 5, "inbox": 5}, passes{"files": 7, "outbox": 5, "inbox": 5})
	second.eventsFake = fake.eventsFake
	second.emitted = nil
	runSweeperOK(t, second)
}

// The retention step names the local ages config.local.json sets against
// config.json's week and shows the outbox's passes as of the last export,
// claiming no row counts.
func TestScenario_SweeperNarratesTheOutboxRetention(t *testing.T) {
	fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, passes{"files": 6, "outbox": 6, "inbox": 5})
	fifth := step(t, runSweeperOK(t, fake), sweeperSteps[4])
	for _, want := range []string{
		"config.local.json sets outbox_age to 2m and inbox_age to 2m for the local stack, where config.json's are 168h and 168h",
		"slab reads no outbox rows",
		"The outbox's passes outbox drained 6 more 0 error 0 backlogged 0",
	} {
		if !strings.Contains(joined(fifth), want) {
			t.Errorf("step 5 does not say %q:\n%s", want, fifth)
		}
	}
}

// The closing step points at Grafana's Explore, the Mimir datasource, and
// the PromQL that sums the passes by reclaimer and outcome; every query
// the run sent went through Grafana's proxy to Mimir and selected the
// service's job.
func TestScenario_SweeperPointsAtTheGrafanaQuery(t *testing.T) {
	fake := newSweeperFake(passes{"files": 5, "outbox": 5, "inbox": 5}, passes{"files": 6, "outbox": 5, "inbox": 5})
	last := step(t, runSweeperOK(t, fake), sweeperSteps[5])
	for _, want := range []string{
		"Observability",
		"Datasource: Mimir",
		`PromQL    : sum by (sweep_reclaimer, sweep_outcome) (sweep_passes_total{job="go-web-service"})`,
	} {
		if !strings.Contains(last, want) {
			t.Errorf("step 6 lacks %q:\n%s", want, last)
		}
	}
	if len(fake.queries) == 0 {
		t.Fatal("the run sent Mimir no query")
	}
	if !slices.ContainsFunc(fake.queries, func(q string) bool { return strings.Contains(q, "sweep_backlogged_ratio") }) {
		t.Errorf("the run never read the backlog gauge: %q", fake.queries)
	}
}
