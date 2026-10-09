//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// The probe bodies, as the SDK writes them: a check's state, and the
// readiness report that lists them, on a 200 and on a 503 problem alike.
type check struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type readiness struct {
	Status string  `json:"status"`
	Checks []check `json:"checks"`
}

const organizations = "/api/organizations"

// seededTotal is the organization count the default state carries.
const seededTotal = 7

// revert leaves the schema empty through one service, the state a startup
// applies the migration set from, and stops that service.
func revert(t *testing.T) {
	t.Helper()
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	integration.Revert(t, s.Client())
	if st := integration.Schema(t, s.Client()); st.Set(integration.AppSet).Version != 0 || st.Ready {
		t.Fatalf("schema after revert = %+v", st)
	}
	s.Stop(t)
}

// assertCurrent asserts the service reports a migrated, seeded database:
// the schema at the head of the set and the seed tree present.
func assertCurrent(t *testing.T, c *webtest.Client) {
	t.Helper()
	st := integration.Schema(t, c)
	app := st.Set(integration.AppSet)
	if app.Version != app.Latest || app.Dirty || len(app.Pending) != 0 || !st.Ready {
		t.Errorf("schema = %+v, want the app set at its head, clean, nothing pending, ready", st)
	}
	if p := webtest.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK); p.total() != seededTotal {
		t.Errorf("organizations total = %d, want %d", p.total(), seededTotal)
	}
}

// Startup applies the migration set to an empty schema and seeds the
// reference data; the probes report every check ready; the seed is
// idempotent on a second run and across a second start; an interrupt
// drains to exit 0. The sweep starts only after the schema: its
// startup pass, which startup's own migration does not gate, runs over the
// tables the migration created, so no pass of the process is refused.
func TestLifecycle_StartupMigratesSeedsAndDrains(t *testing.T) {
	revert(t)

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	live := webtest.Decode[map[string]string](t, c.Get(t, "/healthz"), http.StatusOK)
	if live["status"] != "ok" {
		t.Errorf("healthz = %v", live)
	}
	assertReady(t, c)
	assertCurrent(t, c)

	if n := integration.Seed(t, c, ""); n["organizations"] != 0 {
		t.Errorf("second seed inserted %v, want zero", n)
	}

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}
	if !strings.Contains(s.Output(), "server stopped") {
		t.Errorf("drain not logged:\n%s", s.Output())
	}
	if strings.Contains(s.Output(), refusedRecord) {
		t.Errorf("a sweep pass was refused while the schema was empty; want the sweep started after the schema:\n%s", s.Output())
	}

	// A second start against the seeded database inserts nothing.
	again := integration.Start(t, integration.Options{Seed: integration.Default})
	assertCurrent(t, again.Client())
	if n := integration.Seed(t, again.Client(), ""); n["organizations"] != 0 {
		t.Errorf("seed after a second start inserted %v, want zero", n)
	}
}

// With no set configured the service starts against the schema and
// refuses a seed naming none with a 403 that says why; a named state still
// resets and a named set still seeds, since the policy is the name, not a
// switch.
func TestLifecycle_SeedForbiddenWithNoSet(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	if p.Detail == "" {
		t.Error("403 problem carries no detail")
	}
	if tr := integration.Reset(t, c, "empty"); tr.State != "empty" || !tr.Schema.Ready || tr.Seeded["organizations"] != 0 {
		t.Errorf("reset to empty = %+v", tr)
	}
	if n := integration.Seed(t, c, integration.Default); n["organizations"] != seededTotal {
		t.Errorf("seed default over empty inserted %v, want %d", n, seededTotal)
	}
}

// Two composition roots starting against one empty schema both reach
// ready: the migrator serializes the apply under the schema lock, and the
// seed is idempotent under the unique constraint, so the database ends
// migrated once and seeded once.
func TestLifecycle_ConcurrentStarters(t *testing.T) {
	revert(t)

	a := integration.Launch(t, integration.Options{Seed: integration.Default})
	b := integration.Launch(t, integration.Options{Seed: integration.Default})
	a.Ready(t)
	b.Ready(t)

	assertCurrent(t, a.Client())
	assertCurrent(t, b.Client())
	if code := a.Stop(t); code != 0 {
		t.Errorf("a exit = %d:\n%s", code, a.Output())
	}
	if code := b.Stop(t); code != 0 {
		t.Errorf("b exit = %d:\n%s", code, b.Output())
	}
}
