package database_test

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godb "github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/admin/database"
	"github.com/standards-lab/go-web-service/data"
)

// dialect is the scripted driver's dialect with the server-version
// capability the admin service asserts, so Diagnose runs its whole path.
type dialect struct{ sqltest.Dialect }

func (dialect) ServerVersion() string { return "SELECT version()" }

// module composes the admin domain the way the composition root does, over
// the scripted driver: the pool's lifecycle object, started so it answers
// pings, the session, the migrator over the service's migration set
// (unlocked, since the test dialect has no lock capability), the catalog,
// and the data package as seeder and registry; seed names the state whose
// set the service applies on a seed request naming none.
func module(t *testing.T, seed string, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	cfg := godb.Config{Name: "app"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	pdb := godb.New(pool, cfg)
	if err := pdb.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sdb := sqlate.Wrap(pool, dialect{})
	catalog := query.MustCatalog(query.Patterns(), data.Patterns())
	d := data.New(sdb, catalog)
	m, err := migrate.New(sdb, data.Migrations(), migrate.Options{Unlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := admin.New(pdb, sdb, m, catalog, admin.Options{Seed: seed, Seeder: data.NewSeeder(d), Registry: d})
	r := web.NewRouter()
	r.Mount(web.NewModule(database.Routes(svc)))
	return r, rec
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, want, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func count(n int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"count"}, Rows: [][]driver.Value{{n}}}
}

func TestReads_NeedNoDatabase(t *testing.T) {
	h, _ := module(t, "")

	patterns := decode(t, send(t, h, "GET", "/database/patterns", ""), 200)
	if ns, _ := patterns["namespaces"].([]any); len(ns) != 2 || ns[0] != "app" || ns[1] != "sql" {
		t.Errorf("namespaces = %v; want app and sql", ns)
	}
	statements := decode(t, send(t, h, "GET", "/database/statements", ""), 200)
	domains, _ := statements["domains"].([]any)
	if len(domains) != 1 || domains[0].(map[string]any)["name"] != "data" {
		t.Errorf("domains = %v; want the data package's inventory", domains)
	}
}

func TestSchema_ReportsAnEmptyHistoryAsPending(t *testing.T) {
	// Version: the history table does not exist; Verify: the same.
	h, rec := module(t, "", count(0), count(0))

	st := decode(t, send(t, h, "GET", "/database/schema", ""), 200)
	if st["ready"] != false || st["version"] != float64(0) {
		t.Errorf("status = %v; want not ready at version 0", st)
	}
	if pending, _ := st["pending"].([]any); len(pending) != 1 || pending[0] != float64(1) {
		t.Errorf("pending = %v; want [1]", st["pending"])
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestStates_ListsTheDataPackages(t *testing.T) {
	h, rec := module(t, "")
	res := send(t, h, "GET", "/database/states", "")
	if res.Code != 200 || strings.TrimSpace(res.Body.String()) != `["default","empty"]` {
		t.Errorf("states = %d %s; want default and empty", res.Code, res.Body)
	}
	if len(rec.Calls()) != 0 {
		t.Errorf("the states read touched the database: %v", rec.Ops())
	}
}

func TestSeed_IsForbiddenWithNoSet(t *testing.T) {
	h, _ := module(t, "")
	rec := send(t, h, "POST", "/database/seed", "")
	body := decode(t, rec, 403)
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("Content-Type = %q", ct)
	}
	if detail, _ := body["detail"].(string); !strings.Contains(detail, "disabled") {
		t.Errorf("detail = %q; want the reason carried on a 403", detail)
	}
}

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

// A bodyless seed applies the configured set; a body names another. Both
// answer with the rows inserted by table.
func TestSeed_AppliesTheConfiguredOrNamedSet(t *testing.T) {
	rows := make([]sqltest.Response, 0, 7)
	for i := range 7 {
		rows = append(rows, idRow(string(rune('a'+i))))
	}
	h, rec := module(t, "default", rows...)
	seeded := decode(t, send(t, h, "POST", "/database/seed", ""), 200)
	if seeded["organizations"] != float64(7) {
		t.Errorf("seeded = %v; want seven organizations", seeded)
	}
	seeded = decode(t, send(t, h, "POST", "/database/seed", `{"state":"empty"}`), 200)
	if seeded["organizations"] != float64(0) {
		t.Errorf("seeded = %v; want zero organizations from the empty state", seeded)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestVerbs_RejectBadArgumentsBeforeIO(t *testing.T) {
	cases := map[string]struct {
		path, body, detail string
	}{
		"steps: zero":         {"/database/schema/steps", `{"steps":0}`, "non-zero"},
		"steps: unknown key":  {"/database/schema/steps", `{"step":1}`, "unknown field"},
		"steps: empty body":   {"/database/schema/steps", " ", "empty body"},
		"down: negative":      {"/database/schema/down", `{"steps":-1}`, "positive"},
		"force: negative":     {"/database/schema/force", `{"version":-1}`, "negative"},
		"force: missing body": {"/database/schema/force", "", "empty body"},
		"seed: unknown state": {"/database/seed", `{"state":"nope"}`, `unknown state: "nope"`},
		"state: unknown":      {"/database/state", `{"state":"nope"}`, `unknown state: "nope"`},
		"state: missing body": {"/database/state", "", "empty body"},
		"state: unknown key":  {"/database/state", `{"name":"empty"}`, "unknown field"},
	}
	h, rec := module(t, "default")
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			body := decode(t, send(t, h, "POST", c.path, c.body), 400)
			if detail, _ := body["detail"].(string); !strings.Contains(detail, c.detail) {
				t.Errorf("detail = %q; want it to contain %q", detail, c.detail)
			}
		})
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("rejections reached the database: %v", calls)
	}
}

func TestVerify_OnAnEmptyHistoryIsAConflictWithDetail(t *testing.T) {
	h, rec := module(t, "", count(0))
	body := decode(t, send(t, h, "POST", "/database/schema/verify", ""), 409)
	if detail, _ := body["detail"].(string); !strings.Contains(detail, "pending") {
		t.Errorf("detail = %q; want the pending versions named on a 409", detail)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestDiagnostics_PingsAndReadsTheServerVersion(t *testing.T) {
	h, rec := module(t, "", sqltest.Response{Columns: []string{"version"}, Rows: [][]driver.Value{{"PostgreSQL 18.4"}}})
	d := decode(t, send(t, h, "GET", "/database/diagnostics", ""), 200)
	if d["dialect"] != "test" || d["server_version"] != "PostgreSQL 18.4" {
		t.Errorf("diagnostics = %v", d)
	}
	if ns, _ := d["namespaces"].([]any); len(ns) != 2 {
		t.Errorf("namespaces = %v", ns)
	}
	if _, ok := d["pool"].(map[string]any); !ok {
		t.Errorf("pool counters missing: %v", d)
	}
	if got := rec.SQL(sqltest.OpQuery); len(got) != 1 || got[0] != "SELECT version()" {
		t.Errorf("queries = %v; want the dialect's version statement", got)
	}
}
