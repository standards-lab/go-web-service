//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

type check struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type readiness struct {
	Status string  `json:"status"`
	Checks []check `json:"checks"`
}

const organizations = "/api/organizations"

const seededTotal = 7

func revertToEmptySchema(t *testing.T) {
	t.Helper()
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	integration.Revert(t, s.Client())
	if st := integration.Schema(t, s.Client()); st.Set(integration.AppSet).Version != 0 || st.Ready {
		t.Fatalf("schema after revert = %+v", st)
	}
	s.Stop(t)
}

func assertMigratedAndSeeded(t *testing.T, c *webtest.Client) {
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

func TestLifecycle_StartupMigratesSeedsAndDrains(t *testing.T) {
	revertToEmptySchema(t)

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()

	live := webtest.Decode[map[string]string](t, c.Get(t, "/healthz"), http.StatusOK)
	if live["status"] != "ok" {
		t.Errorf("healthz = %v", live)
	}
	assertReady(t, c)
	assertMigratedAndSeeded(t, c)

	if n := integration.Seed(t, c, ""); n["organizations"] != 0 {
		t.Errorf("second seed inserted %v, want zero: the seed is idempotent", n)
	}

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}
	if !strings.Contains(s.Output(), "server stopped") {
		t.Errorf("drain not logged:\n%s", s.Output())
	}
	if refused(s.Output()) > 0 {
		t.Errorf("a sweep pass was refused while the schema was empty; want the sweep started after the schema:\n%s", s.Output())
	}

	again := integration.Start(t, integration.Options{Seed: integration.Default})
	assertMigratedAndSeeded(t, again.Client())
	if n := integration.Seed(t, again.Client(), ""); n["organizations"] != 0 {
		t.Errorf("seed after a second start inserted %v, want zero", n)
	}
}

func TestLifecycle_NoSetForbidsAnUnnamedSeedNotANamedOne(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	if p.Detail == "" {
		t.Error("403 problem carries no detail; want it to say why")
	}
	if tr := integration.Reset(t, c, "empty"); tr.State != "empty" || !tr.Schema.Ready || tr.Seeded["organizations"] != 0 {
		t.Errorf("reset to empty = %+v", tr)
	}
	if n := integration.Seed(t, c, integration.Default); n["organizations"] != seededTotal {
		t.Errorf("seed default over empty inserted %v, want %d", n, seededTotal)
	}
}

func TestLifecycle_ConcurrentStartersMigrateAndSeedOnce(t *testing.T) {
	revertToEmptySchema(t)

	a := integration.Launch(t, integration.Options{Seed: integration.Default})
	b := integration.Launch(t, integration.Options{Seed: integration.Default})
	a.Ready(t)
	b.Ready(t)

	assertMigratedAndSeeded(t, a.Client())
	assertMigratedAndSeeded(t, b.Client())
	if code := a.Stop(t); code != 0 {
		t.Errorf("a exit = %d:\n%s", code, a.Output())
	}
	if code := b.Stop(t); code != 0 {
		t.Errorf("b exit = %d:\n%s", code, b.Output())
	}
}
