//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// The probe bodies, as the SDK writes them.
type readiness struct {
	Status string `json:"status"`
	Checks []struct {
		Name  string `json:"name"`
		Ready bool   `json:"ready"`
	} `json:"checks"`
}

type page struct {
	Total int `json:"total"`
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
	if st := integration.Schema(t, s.Client()); st.Version != 0 || st.Ready {
		t.Fatalf("schema after revert = %+v", st)
	}
	s.Stop(t)
}

// assertCurrent asserts the service reports a migrated, seeded database:
// the schema at the head of the set and the seed tree present.
func assertCurrent(t *testing.T, c *webtest.Client) {
	t.Helper()
	st := integration.Schema(t, c)
	if st.Version != 1 || st.Dirty || len(st.Pending) != 0 || !st.Ready {
		t.Errorf("schema = %+v, want version 1, clean, nothing pending, ready", st)
	}
	if p := webtest.Decode[page](t, c.Get(t, organizations), http.StatusOK); p.Total != seededTotal {
		t.Errorf("organizations total = %d, want %d", p.Total, seededTotal)
	}
}

// Startup applies the migration set to an empty schema and seeds the
// reference data; the probes report every stage ready; the seed is
// idempotent on a second run and across a second start; an interrupt
// drains to exit 0.
func TestLifecycle_StartupMigratesSeedsAndDrains(t *testing.T) {
	revert(t)

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	live := webtest.Decode[map[string]string](t, c.Get(t, "/healthz"), http.StatusOK)
	if live["status"] != "ok" {
		t.Errorf("healthz = %v", live)
	}
	ready := webtest.Decode[readiness](t, c.Get(t, "/readyz"), http.StatusOK)
	if ready.Status != "ready" {
		t.Errorf("readyz status = %q", ready.Status)
	}
	want := map[string]bool{"lifecycle": false, "database": false, "schema": false}
	for _, ch := range ready.Checks {
		if _, known := want[ch.Name]; !known || !ch.Ready {
			t.Errorf("readyz check %s ready=%t", ch.Name, ch.Ready)
		}
		want[ch.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("readyz check %s missing", name)
		}
	}
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
