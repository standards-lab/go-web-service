//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// The admin mount's read shapes, the fields the suite asserts.
type diagnostics struct {
	Dialect       string             `json:"dialect"`
	Ping          int64              `json:"ping"`
	ServerVersion string             `json:"server_version"`
	Pool          struct{ Open int } `json:"pool"`
	Namespaces    []string           `json:"namespaces"`
}

type catalog struct {
	Namespaces []string `json:"namespaces"`
	Patterns   []struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Tier      string `json:"tier"`
		Native    string `json:"native"`
	} `json:"patterns"`
}

type inventory struct {
	Domains []struct {
		Name       string `json:"name"`
		Statements []struct {
			Name                string `json:"name"`
			Tier                string `json:"tier"`
			TransactionRequired bool   `json:"transaction_required"`
			Key                 string `json:"key"`
		} `json:"statements"`
	} `json:"domains"`
}

const admin = "/admin/database"

func TestAdmin(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)

	schema := func(t *testing.T, res *webtest.Response) integration.SchemaStatus {
		t.Helper()
		return webtest.Decode[integration.SchemaStatus](t, res, http.StatusOK)
	}
	assertCurrentSchema := func(t *testing.T, st integration.SchemaStatus) {
		t.Helper()
		app := st.Set(integration.AppSet)
		if app.Version != 1 || app.Dirty || len(app.Pending) != 0 || !st.Ready {
			t.Errorf("schema = %+v, want the app set at version 1, clean, nothing pending, ready", st)
		}
	}
	assertEmptySchema := func(t *testing.T, st integration.SchemaStatus) {
		t.Helper()
		app := st.Set(integration.AppSet)
		if app.Version != 0 || app.Dirty || len(app.Pending) != 1 || app.Pending[0] != 1 || st.Ready {
			t.Errorf("schema = %+v, want the app set at version 0 with 1 pending, not ready", st)
		}
	}
	appSet := func(body map[string]any) map[string]any {
		body["set"] = integration.AppSet
		return body
	}

	t.Run("diagnostics", func(t *testing.T) {
		d := webtest.Decode[diagnostics](t, c.Get(t, admin+"/diagnostics"), http.StatusOK)
		if d.Dialect != "postgres" || d.Ping <= 0 || !strings.HasPrefix(d.ServerVersion, "PostgreSQL 18") || d.Pool.Open < 1 {
			t.Errorf("diagnostics = %+v", d)
		}
		if !equal(d.Namespaces, []string{"app", "blobfs", "sql"}) {
			t.Errorf("namespaces = %v", d.Namespaces)
		}
	})

	t.Run("schema", func(t *testing.T) {
		st := schema(t, c.Get(t, admin+"/schema"))
		assertCurrentSchema(t, st)
		// blobfs's set is declared beneath the service's own, and current.
		if len(st.Sets) != 2 || st.Sets[0].Name != "blobfs" || st.Sets[1].Name != integration.AppSet {
			t.Errorf("sets = %+v, want blobfs then app", st.Sets)
		}
		if bf := st.Set("blobfs"); bf.Version != bf.Latest || bf.Latest < 2 || bf.Dirty {
			t.Errorf("blobfs set = %+v, want current at its head", bf)
		}
		ms := st.Set(integration.AppSet).Migrations
		if len(ms) != 1 || ms[0].Version != 1 || ms[0].Name != "organization" || !ms[0].Applied || !ms[0].Transactional {
			t.Errorf("migrations = %+v", ms)
		}
	})

	t.Run("patterns", func(t *testing.T) {
		cat := webtest.Decode[catalog](t, c.Get(t, admin+"/patterns"), http.StatusOK)
		if !equal(cat.Namespaces, []string{"app", "blobfs", "sql"}) {
			t.Errorf("namespaces = %v", cat.Namespaces)
		}
		found := false
		for _, p := range cat.Patterns {
			if p.Namespace == "app" && p.Name == "identity" {
				found = true
				if p.Tier != "native" || p.Native == "" {
					t.Errorf("app.identity = %+v, want the native tier with its note", p)
				}
			}
		}
		if !found {
			t.Error("app.identity missing from the catalog")
		}
	})

	t.Run("statements", func(t *testing.T) {
		inv := webtest.Decode[inventory](t, c.Get(t, admin+"/statements"), http.StatusOK)
		names := make([]string, len(inv.Domains))
		for i, d := range inv.Domains {
			names[i] = d.Name
		}
		if !equal(names, []string{"data", "organization"}) {
			t.Fatalf("domains = %v", names)
		}
		byName := map[string]map[string]struct {
			tier, key string
			tx        bool
		}{}
		for _, d := range inv.Domains {
			byName[d.Name] = map[string]struct {
				tier, key string
				tx        bool
			}{}
			for _, st := range d.Statements {
				byName[d.Name][st.Name] = struct {
					tier, key string
					tx        bool
				}{st.Tier, st.Key, st.TransactionRequired}
			}
		}
		if lock := byName["data"]["lock"]; lock.tier != "native" || !lock.tx {
			t.Errorf("data.lock = %+v, want native and transaction required", lock)
		}
		if view := byName["organization"]["organization_view"]; view.tier != "standard" || view.key != "id" {
			t.Errorf("organization_view = %+v, want standard with key id", view)
		}
		for _, name := range []string{"create", "edit", "transfer", "delete", "version", "in_subtree"} {
			if _, ok := byName["organization"][name]; !ok {
				t.Errorf("organization.%s missing from the inventory", name)
			}
		}
	})

	t.Run("verify and up on a current schema", func(t *testing.T) {
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/verify", nil)))
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/up", nil)))
	})

	t.Run("down, verify pending, up", func(t *testing.T) {
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/down", appSet(map[string]any{}))))
		if p := c.Post(t, admin+"/schema/verify", nil).Problem(t, http.StatusConflict); !strings.Contains(p.Detail, "pending") {
			t.Errorf("verify on a pending schema: detail = %q", p.Detail)
		}
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/down", appSet(map[string]any{})))) // nothing applied: a no-op
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/up", nil)))
	})

	t.Run("steps", func(t *testing.T) {
		_ = c.Post(t, admin+"/schema/steps", appSet(map[string]any{"steps": 0})).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, admin+"/schema/down", appSet(map[string]any{"steps": -1})).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, admin+"/schema/down", nil).Problem(t, http.StatusBadRequest)                                // the set is required
		_ = c.Post(t, admin+"/schema/steps", appSet(map[string]any{"step": 1})).Problem(t, http.StatusBadRequest) // unknown field
		if p := c.Post(t, admin+"/schema/steps", map[string]any{"set": "nope", "steps": 1}).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "nope") {
			t.Errorf("an undeclared set: detail = %q, want it named", p.Detail)
		}
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/steps", appSet(map[string]any{"steps": -1}))))
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/steps", appSet(map[string]any{"steps": 1}))))
	})

	t.Run("force", func(t *testing.T) {
		_ = c.Post(t, admin+"/schema/force", appSet(map[string]any{"version": -1})).Problem(t, http.StatusBadRequest)
		_ = c.Post(t, admin+"/schema/force", appSet(map[string]any{"version": 7})).Problem(t, http.StatusBadRequest) // outside the set
		_ = c.Post(t, admin+"/schema/force", map[string]any{"version": 1}).Problem(t, http.StatusBadRequest)         // the set is required
		// Force sets the history without touching the schema: to 0 the set
		// reads as pending though the table stands; back to 1 it is current.
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/force", appSet(map[string]any{"version": 0}))))
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/force", appSet(map[string]any{"version": 1}))))
	})

	t.Run("seed", func(t *testing.T) {
		// The schema cases above recreated the table, so it is empty here.
		integration.Revert(t, c)
		schema(t, c.Post(t, admin+"/schema/up", nil))
		if n := integration.Seed(t, c, ""); n["organizations"] != seededTotal {
			t.Errorf("seed on an empty table inserted %v, want %d", n, seededTotal)
		}
		if n := integration.Seed(t, c, ""); n["organizations"] != 0 {
			t.Errorf("seed on a seeded table inserted %v, want zero", n)
		}
	})

	t.Run("states", func(t *testing.T) {
		if got := integration.States(t, c); !equal(got, []string{"default", "empty"}) {
			t.Errorf("states = %v, want default and empty", got)
		}
	})

	t.Run("state", func(t *testing.T) {
		total := func(t *testing.T) int {
			t.Helper()
			return webtest.Decode[page](t, c.Get(t, organizations), http.StatusOK).total()
		}
		// A reset without its confirmation is refused before it touches the
		// schema.
		if p := c.Post(t, admin+"/state", map[string]string{"state": "empty"}).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, `"confirm": true`) {
			t.Errorf("unconfirmed reset: detail = %q", p.Detail)
		}
		if total(t) != seededTotal {
			t.Errorf("an unconfirmed reset changed the data: total %d", total(t))
		}
		// From the seeded tree to empty: the schema is rebuilt and current,
		// the set inserted nothing, and the table is empty.
		tr := integration.Reset(t, c, "empty")
		if tr.State != "empty" || tr.Seeded["organizations"] != 0 || total(t) != 0 {
			t.Errorf("reset to empty = %+v, total %d", tr, total(t))
		}
		assertCurrentSchema(t, schema(t, c.Get(t, admin+"/schema")))
		// A named set applies over the empty state without a reset.
		if n := integration.Seed(t, c, integration.Default); n["organizations"] != seededTotal || total(t) != seededTotal {
			t.Errorf("seed default over empty inserted %v, total %d", n, total(t))
		}
		// Back to default from the seeded tree: rebuilt and seeded again.
		tr = integration.Reset(t, c, integration.Default)
		if tr.State != "default" || tr.Seeded["organizations"] != seededTotal || total(t) != seededTotal {
			t.Errorf("reset to default = %+v, total %d", tr, total(t))
		}
		// An undeclared name is a 400 that names it, on both routes.
		for _, path := range []string{admin + "/state", admin + "/seed"} {
			body := map[string]any{"state": "nope"}
			if path == admin+"/state" {
				body["confirm"] = true
			}
			if p := c.Post(t, path, body).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, `unknown state: "nope"`) {
				t.Errorf("%s nope: detail = %q", path, p.Detail)
			}
		}
	})

	assertCurrentSchema(t, schema(t, c.Get(t, admin+"/schema")))
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
