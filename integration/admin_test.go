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

type schemaStatus struct {
	integration.SchemaStatus
	Migrations []struct {
		Version       int    `json:"version"`
		Name          string `json:"name"`
		Transactional bool   `json:"transactional"`
		Applied       bool   `json:"applied"`
	} `json:"migrations"`
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
	s := integration.Start(t, integration.Options{Seed: true})
	c := s.Client()
	integration.Reset(t, c)

	schema := func(t *testing.T, res *webtest.Response) schemaStatus {
		t.Helper()
		return webtest.Decode[schemaStatus](t, res, http.StatusOK)
	}
	assertCurrentSchema := func(t *testing.T, st schemaStatus) {
		t.Helper()
		if st.Version != 1 || st.Dirty || len(st.Pending) != 0 || !st.Ready {
			t.Errorf("schema = %+v, want version 1, clean, nothing pending, ready", st.SchemaStatus)
		}
	}
	assertEmptySchema := func(t *testing.T, st schemaStatus) {
		t.Helper()
		if st.Version != 0 || st.Dirty || len(st.Pending) != 1 || st.Pending[0] != 1 || st.Ready {
			t.Errorf("schema = %+v, want version 0 with 1 pending, not ready", st.SchemaStatus)
		}
	}

	t.Run("diagnostics", func(t *testing.T) {
		d := webtest.Decode[diagnostics](t, c.Get(t, admin+"/diagnostics"), http.StatusOK)
		if d.Dialect != "postgres" || d.Ping <= 0 || !strings.HasPrefix(d.ServerVersion, "PostgreSQL 18") || d.Pool.Open < 1 {
			t.Errorf("diagnostics = %+v", d)
		}
		if !equal(d.Namespaces, []string{"app", "sql"}) {
			t.Errorf("namespaces = %v", d.Namespaces)
		}
	})

	t.Run("schema", func(t *testing.T) {
		st := schema(t, c.Get(t, admin+"/schema"))
		assertCurrentSchema(t, st)
		if len(st.Migrations) != 1 || st.Migrations[0].Version != 1 || st.Migrations[0].Name != "organization" || !st.Migrations[0].Applied || !st.Migrations[0].Transactional {
			t.Errorf("migrations = %+v", st.Migrations)
		}
	})

	t.Run("patterns", func(t *testing.T) {
		cat := webtest.Decode[catalog](t, c.Get(t, admin+"/patterns"), http.StatusOK)
		if !equal(cat.Namespaces, []string{"app", "sql"}) {
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
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/down", nil)))
		if p := c.Post(t, admin+"/schema/verify", nil).Problem(t, http.StatusConflict); !strings.Contains(p.Detail, "pending") {
			t.Errorf("verify on a pending schema: detail = %q", p.Detail)
		}
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/down", nil))) // nothing applied: a no-op
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/up", nil)))
	})

	t.Run("steps", func(t *testing.T) {
		c.Post(t, admin+"/schema/steps", map[string]int{"steps": 0}).Problem(t, http.StatusBadRequest)
		c.Post(t, admin+"/schema/down", map[string]int{"steps": 0}).Problem(t, http.StatusBadRequest)
		c.Post(t, admin+"/schema/steps", map[string]int{"step": 1}).Problem(t, http.StatusBadRequest) // unknown field
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/steps", map[string]int{"steps": -1})))
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/steps", map[string]int{"steps": 1})))
	})

	t.Run("force", func(t *testing.T) {
		c.Post(t, admin+"/schema/force", map[string]int{"version": -1}).Problem(t, http.StatusBadRequest)
		c.Post(t, admin+"/schema/force", map[string]int{"version": 7}).Problem(t, http.StatusBadRequest) // outside the set
		// Force sets the history without touching the schema: to 0 the set
		// reads as pending though the table stands; back to 1 it is current.
		assertEmptySchema(t, schema(t, c.Post(t, admin+"/schema/force", map[string]int{"version": 0})))
		assertCurrentSchema(t, schema(t, c.Post(t, admin+"/schema/force", map[string]int{"version": 1})))
	})

	t.Run("seed", func(t *testing.T) {
		// The schema cases above recreated the table, so it is empty here.
		integration.Revert(t, c)
		schema(t, c.Post(t, admin+"/schema/up", nil))
		if n := integration.Seed(t, c); n["organizations"] != seededTotal {
			t.Errorf("seed on an empty table inserted %v, want %d", n, seededTotal)
		}
		if n := integration.Seed(t, c); n["organizations"] != 0 {
			t.Errorf("seed on a seeded table inserted %v, want zero", n)
		}
	})

	assertCurrentSchema(t, schema(t, c.Get(t, admin+"/schema")))
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}
