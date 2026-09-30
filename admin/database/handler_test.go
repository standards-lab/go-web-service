package database_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	godb "github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/admin/database"
	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// dialect is the scripted driver's dialect with the server-version
// capability the admin service asserts, so Diagnose runs its whole path.
type dialect struct{ sqltest.Dialect }

func (dialect) ServerVersion() string { return "SELECT version()" }

// module composes the admin domain the way the composition root does, over
// the scripted driver: the pool's lifecycle object, started so it answers
// pings, the session, the migrator over the service's migration set
// (unlocked, since the test dialect has no lock capability), the catalog,
// the data package's seeder over stand-ins for the domains' seed
// contributions, and the registry; seed names the state whose
// set the service applies on a seed request naming none. The schema gate
// is an open one no sweep holds.
func module(t *testing.T, seed string, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
	t.Helper()
	return gatedModule(t, new(sdk.Gate), seed, responses...)
}

// gatedModule is module over the given schema gate, which a test holds
// shared as a sweep pass in flight would.
func gatedModule(t *testing.T, gate database.SchemaGate, seed string, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
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
	svc := admin.New(pdb, sdb, m, catalog, admin.Options{Seed: seed, Seeder: data.NewSeeder(d, nil, organizations{}, files{"logos"}, files{"documents"}), Registry: d})
	r := web.NewRouter()
	r.Mount(web.NewModule(database.Routes(svc, gate, slog.New(slog.DiscardHandler))))
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
	// The set's history table does not exist: one probe reports it.
	h, rec := module(t, "", count(0))

	st := decode(t, send(t, h, "GET", "/database/schema", ""), 200)
	sets, _ := st["sets"].([]any)
	if st["ready"] != false || len(sets) != 1 {
		t.Fatalf("status = %v; want not ready with one set", st)
	}
	app, _ := sets[0].(map[string]any)
	if app["name"] != data.AppSet || app["version"] != float64(0) {
		t.Errorf("set = %v; want the app set at version 0", app)
	}
	head := len(data.Migrations()[0].Migrations)
	pending, _ := app["pending"].([]any)
	for i, v := range pending {
		if v != float64(i+1) {
			t.Errorf("pending = %v; want every version, 1 through %d", pending, head)
		}
	}
	if len(pending) != head {
		t.Errorf("pending = %v; want every version, 1 through %d", pending, head)
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

// organizations stands in for the organization domain's seed
// contribution, which the composition root hands the seeder: it reads the
// state's organizations and reports each as inserted, running no
// statement, since the domain's own tests prove its seed.
type organizations struct{}

func (organizations) Key() string { return "organizations" }
func (organizations) Apply(_ context.Context, _ *sqlate.Tx, raw json.RawMessage) (int, error) {
	rows, err := data.SeedRows[json.RawMessage](raw)
	return len(rows), err
}

// files stands in for a domain's contribution of stored files under key:
// it reads the state's rows and reports each as stored, writing nothing.
type files struct{ key string }

func (f files) Key() string { return f.key }
func (files) Write(_ context.Context, raw json.RawMessage, _ fs.FS) (int, error) {
	rows, err := data.SeedRows[json.RawMessage](raw)
	return len(rows), err
}

// A bodyless seed applies the configured set; a body names another. Both
// answer with the rows inserted by table.
func TestSeed_AppliesTheConfiguredOrNamedSet(t *testing.T) {
	h, rec := module(t, "default")
	seeded := decode(t, send(t, h, "POST", "/database/seed", ""), 200)
	if seeded["organizations"] != float64(7) || seeded["logos"] != float64(7) || seeded["documents"] != float64(1) {
		t.Errorf("seeded = %v; want seven organizations, seven logos, and one tree", seeded)
	}
	seeded = decode(t, send(t, h, "POST", "/database/seed", `{"state":"empty"}`), 200)
	if seeded["organizations"] != float64(0) || seeded["logos"] != float64(0) || seeded["documents"] != float64(0) {
		t.Errorf("seeded = %v; want every contribution at zero from the empty state", seeded)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestVerbs_RejectBadArgumentsBeforeIO(t *testing.T) {
	cases := map[string]struct {
		path, body, detail string
	}{
		"steps: zero":           {"/database/schema/steps", `{"set":"app","steps":0}`, "non-zero"},
		"steps: unknown key":    {"/database/schema/steps", `{"set":"app","step":1}`, "unknown field"},
		"steps: empty body":     {"/database/schema/steps", " ", "empty body"},
		"steps: no set":         {"/database/schema/steps", `{"steps":1}`, "set"},
		"steps: unknown set":    {"/database/schema/steps", `{"set":"nope","steps":1}`, "nope"},
		"down: negative":        {"/database/schema/down", `{"set":"app","steps":-1}`, "positive"},
		"down: missing body":    {"/database/schema/down", "", "empty body"},
		"down: unknown set":     {"/database/schema/down", `{"set":"nope"}`, "nope"},
		"force: negative":       {"/database/schema/force", `{"set":"app","version":-1}`, "is not in set"},
		"force: not in the set": {"/database/schema/force", `{"set":"app","version":999}`, "version 999 is not in set"},
		"force: missing body":   {"/database/schema/force", "", "empty body"},
		"force: unknown set":    {"/database/schema/force", `{"set":"nope","version":1}`, "nope"},
		"seed: unknown state":   {"/database/seed", `{"state":"nope"}`, `unknown state: "nope"`},
		"state: unknown":        {"/database/state", `{"state":"nope","confirm":true}`, `unknown state: "nope"`},
		"state: unconfirmed":    {"/database/state", `{"state":"default"}`, `"confirm": true`},
		"state: confirm false":  {"/database/state", `{"state":"default","confirm":false}`, `"confirm": true`},
		"state: missing body":   {"/database/state", "", "empty body"},
		"state: unknown key":    {"/database/state", `{"name":"empty","confirm":true}`, "unknown field"},
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

// sendWithin is send under a request context that ends after d, as a
// client that gives up would.
func sendWithin(t *testing.T, h http.Handler, d time.Duration, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// A reset waits for the sweep pass in flight: while a pass holds the gate
// shared, the reset reaches no statement, and once the pass releases it,
// the reset runs, here into a scripted failure, and releases the gate in
// turn.
func TestState_WaitsForTheSweepPassInFlight(t *testing.T) {
	gate := new(sdk.Gate)
	pass, err := gate.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h, rec := gatedModule(t, gate, "default", sqltest.Response{Err: errors.New("scripted: the revert failed")})

	done := make(chan int, 1)
	go func() { done <- send(t, h, "POST", "/database/state", `{"state":"default","confirm":true}`).Code }()
	time.Sleep(50 * time.Millisecond)
	select {
	case code := <-done:
		t.Fatalf("the reset answered %d under a pass in flight, want it waiting", code)
	default:
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("the reset reached the database under a pass in flight: %v", calls)
	}

	pass()
	select {
	case code := <-done:
		if code != http.StatusInternalServerError {
			t.Errorf("reset = %d, want the scripted failure's 500", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reset did not run once the pass released the gate")
	}
	if len(rec.Calls()) == 0 {
		t.Error("the reset reached no statement once the gate was free")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	next, err := gate.Shared(ctx)
	if err != nil {
		t.Fatalf("the next pass after the reset: %v; want the gate released", err)
	}
	next()
}

// Every verb that changes the schema holds the gate: with a pass holding
// it, each waits until its request gives up, having run nothing. verify,
// force, and seed hold nothing, so each answers under the pass.
func TestVerbs_ChangingTheSchemaHoldTheGate(t *testing.T) {
	gate := new(sdk.Gate)
	pass, err := gate.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pass()
	h, rec := gatedModule(t, gate, "default", count(0))

	for path, body := range map[string]string{
		"/database/schema/up":    "",
		"/database/schema/down":  `{"set":"app"}`,
		"/database/schema/steps": `{"set":"app","steps":1}`,
		"/database/state":        `{"state":"default","confirm":true}`,
	} {
		if code := sendWithin(t, h, 30*time.Millisecond, path, body).Code; code != http.StatusInternalServerError {
			t.Errorf("%s under a pass = %d, want the abandoned wait's 500", path, code)
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("a verb waiting on the gate reached the database: %v", calls)
	}

	decode(t, send(t, h, "POST", "/database/schema/verify", ""), http.StatusConflict)
	decode(t, send(t, h, "POST", "/database/schema/force", `{"set":"nope","version":1}`), http.StatusBadRequest)
	decode(t, send(t, h, "POST", "/database/seed", `{"state":"nope"}`), http.StatusBadRequest)
}
