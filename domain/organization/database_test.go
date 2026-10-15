package organization_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-messaging/outbox"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/domain/organization"
)

const (
	validID  = "00000000-0000-7000-8000-000000000000"
	parentID = "00000000-0000-7000-8000-000000000001"
)

// service builds the domain over the scripted driver the way the
// composition root builds it over the pool: statements compiled and handles
// bound at construction, no I/O.
func service(t *testing.T, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder) {
	t.Helper()
	svc, rec, _ := serviceOver(t, sqltest.Dialect{}, responses...)
	return svc, rec
}

// serviceOver builds the domain over the scripted driver in dialect, with
// blobfs's store compiled against the same catalog, and the object store
// over the fake, started as the composition root starts the real one.
func serviceOver(t *testing.T, dialect sqlate.Dialect, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	return serviceLogging(t, slog.New(slog.DiscardHandler), dialect, responses...)
}

// serviceLogging is serviceOver with the service logging to logger.
func serviceLogging(t *testing.T, logger *slog.Logger, dialect sqlate.Dialect, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, dialect)
	if err != nil {
		t.Fatal(err)
	}
	fake := storagetest.NewFake()
	cfg := storage.Config{Container: "test"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	objects := storage.New(fake, cfg)
	if err := objects.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Shutdown(context.Background()) })
	db := data.New(sqlate.Wrap(pool, dialect), catalog)
	return organization.New(db, data.NewStorage(db, fs, objects), logger, recorder(t)), rec, fake
}

// eventID and eventTime are what the tests' recorder stamps on every
// event it emits.
const eventID = "00000000-0000-7000-8000-0000000000ee"

var eventTime = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// recorder is the recorder the composition root injects, go-messaging's
// over its Postgres outbox, with the id and the time it stamps fixed: its
// event's write is one statement on the command's transaction, which the
// scripted driver records.
func recorder(t *testing.T) *organization.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return event.NewRecorder(ob.Sink(), "web-service",
		event.IDs(func() string { return eventID }), event.Clock(func() time.Time { return eventTime }))
}

// seeder composes the data package's seeder over the layer's row
// contribution, as the composition root does, beside stand-ins for the
// contributions of stored files the default state also carries, which
// write nothing: the logo seed's own tests are storage_test.go's.
func seeder(t *testing.T, responses ...sqltest.Response) (*data.Seeder, *sqltest.Recorder) {
	t.Helper()
	s, _, rec := seederOver(t, responses...)
	return s, rec
}

// seederOver is seeder with the database the layer registered its
// statements on.
func seederOver(t *testing.T, responses ...sqltest.Response) (*data.Seeder, *data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog)
	svc := organization.New(db, data.NewStorage(db, fs, nil), slog.New(slog.DiscardHandler), recorder(t))
	return data.NewSeeder(db, svc.Seed(), noFiles("logos"), noFiles("documents")), db, rec
}

// blobfsStatements is what blobfs's store prepares when it verifies
// itself, over a pool of its own, so a test can tell the layer's probes
// from blobfs's.
func blobfsStatements(t *testing.T) []string {
	t.Helper()
	fs, err := bfdata.New(query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns()), sqltest.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	pool, rec := sqltest.Open(t)
	if err := fs.Verify(context.Background(), sqlate.Wrap(pool, sqltest.Dialect{})); err != nil {
		t.Fatal(err)
	}
	return rec.SQL(sqltest.OpPrepare)
}

// noFiles stands in for a contribution of stored files under its key,
// storing nothing.
type noFiles string

func (k noFiles) Key() string { return string(k) }
func (noFiles) Write(context.Context, json.RawMessage, fs.FS) (int, error) {
	return 0, nil
}

func identity(id string, version int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"id", "version"}, Rows: [][]driver.Value{{id, version}}}
}

func count(n int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"count"}, Rows: [][]driver.Value{{n}}}
}

// exists is the organization's key lookup finding it.
func exists() sqltest.Response {
	return sqltest.Response{Columns: []string{"version"}, Rows: [][]driver.Value{{int64(1)}}}
}

// stored is the stored statement's read of the organization after the
// command that changed it, a root unless parent is given.
func stored(id, code, name string, version int64, parent ...string) sqltest.Response {
	var p driver.Value
	if len(parent) > 0 {
		p = parent[0]
	}
	return sqltest.Response{
		Columns: []string{"organization_id", "parent_id", "code", "name", "version"},
		Rows:    [][]driver.Value{{id, p, code, name, version}},
	}
}

// written is the outbox's insert of a command's event.
func written() sqltest.Response { return sqltest.Response{Affected: 1} }

func row() sqltest.Response {
	now := time.Now()
	return sqltest.Response{
		Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"},
		Rows:    [][]driver.Value{{validID, nil, "acme", "Acme", int64(1), now, now, "/acme"}},
	}
}

// The wiring test: every handle binds once with sample arguments, so a key
// that does not match its file's parameters, an argument count the
// statement does not bind, or a scan out of step with the SELECT list fails
// here rather than on a request. The strict driver checks the placeholder
// count on every call.
func TestStore_EveryHandleBindsItsFilesParameters(t *testing.T) {
	ctx := context.Background()
	s, rec := service(t,
		sqltest.WithTotal(row(), 1),                // list: the page and its window count in one statement
		row(),                                      // find by id
		identity(validID, 1),                       // create
		stored(validID, "eng", "Eng", 1),           // create: the stored row
		written(),                                  // create: the event
		sqltest.Response{Affected: 1},              // edit
		stored(validID, "eng", "Eng", 2),           // edit: the stored row
		written(),                                  // edit: the event
		sqltest.Response{Affected: 0},              // transfer: lock
		count(0),                                   // transfer: in_subtree
		sqltest.Response{Affected: 1},              // transfer: update
		stored(validID, "eng", "Eng", 3, parentID), // transfer: the stored row
		written(),                                  // transfer: the event
		sqltest.Response{Affected: 1},              // delete
		written(),                                  // delete: the event
	)
	lq, _ := web.ParseQuery(url.Values{"code": {"acme"}, "sort": {"-path"}}, web.Limits{DefaultSize: 20, MaxSize: 100})
	if items, paging, err := s.List(ctx, lq); err != nil || (paging.Total == nil || *paging.Total != 1) || paging.More || items[0].Path != "/acme" {
		t.Fatalf("List = %v, %+v, %v", items, paging, err)
	}
	if o, err := s.Find(ctx, validID); err != nil || o.Code != "acme" || o.ParentID != nil {
		t.Fatalf("Find = %+v, %v", o, err)
	}
	if id, err := s.Create(ctx, organization.CreateOrganization{Code: "eng", Name: "Eng"}); err != nil || id.Version != 1 {
		t.Fatalf("Create = %+v, %v", id, err)
	}
	if id, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "eng", Name: "Eng"}); err != nil || id.Version != 2 {
		t.Fatalf("Edit = %+v, %v", id, err)
	}
	if id, err := s.Transfer(ctx, validID, 2, transferTo(t, `{"parent_id":"`+parentID+`"}`)); err != nil || id.Version != 3 {
		t.Fatalf("Transfer = %+v, %v", id, err)
	}
	if err := s.Delete(ctx, validID, 3); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending = %d, leaked = %d", rec.Pending(), rec.RowsLeaked())
	}
	sqls := rec.SQL(sqltest.OpQuery)
	if !strings.Contains(sqls[0], "COUNT(*) OVER () AS sqlate_total FROM (WITH RECURSIVE lineage") || !strings.Contains(sqls[0], "WHERE q.code = CAST($1 AS text)) q ORDER BY q.path DESC, q.id DESC OFFSET $2") {
		t.Errorf("list = %q; want the page and its window count in one statement", sqls[0])
	}
	if create := sqls[2]; !strings.HasSuffix(create, "RETURNING id, version") {
		t.Errorf("create = %q; want the identity pattern spliced", create)
	}
	if lock := rec.Calls()[13]; lock.SQL != "SELECT pg_advisory_xact_lock(hashtext($1))" || lock.Args[0] != data.LockOrganizationTree {
		t.Errorf("transfer did not take the named tree lock first: %+v", lock)
	}
	// Each command is one transaction: its change, its stored row's read
	// when it leaves a row, and its event.
	sameOps(t, rec, q, q,
		begin, q, q, x, commit, // create
		begin, x, q, x, commit, // edit
		begin, x, q, x, q, x, commit, // transfer
		begin, x, x, commit, // delete
	)
}

func TestStore_TransferRejectsACycleBeforeTheUpdate(t *testing.T) {
	s, rec := service(t, sqltest.Response{Affected: 0}, count(1))
	_, err := s.Transfer(context.Background(), validID, 1, transferTo(t, `{"parent_id":"`+parentID+`"}`))
	if !errors.Is(err, organization.ErrCycle) {
		t.Fatalf("err = %v; want ErrCycle", err)
	}
	if ops := rec.Ops(); ops[len(ops)-1] != sqltest.OpRollback {
		t.Errorf("ops = %v; want a rollback", ops)
	}
}

func TestStore_TransferToRootBindsNull(t *testing.T) {
	s, rec := service(t, sqltest.Response{Affected: 0}, sqltest.Response{Affected: 1}, stored(validID, "eng", "Eng", 2), written())
	if _, err := s.Transfer(context.Background(), validID, 1, transferTo(t, `{"parent_id":null}`)); err != nil {
		t.Fatal(err)
	}
	update := rec.Calls()[2]
	queries := rec.SQL(sqltest.OpQuery)
	if update.Args[0] != nil || len(queries) != 1 || !strings.HasPrefix(queries[0], "SELECT id AS organization_id") {
		t.Errorf("root transfer: args %v, queries %v; want a null parent and no cycle walk, only the stored row's read", update.Args, queries)
	}
}

func TestStore_GuardDistinguishesAbsentFromStale(t *testing.T) {
	ctx := context.Background()
	s, _ := service(t,
		sqltest.Response{Affected: 0}, sqltest.Response{Columns: []string{"version"}}, // absent
		sqltest.Response{Affected: 0}, sqltest.Response{Columns: []string{"version"}, Rows: [][]driver.Value{{int64(5)}}}, // stale
	)
	if _, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "a", Name: "A"}); err == nil || errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("absent row: err = %v; want the missing-row error", err)
	}
	if _, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "a", Name: "A"}); !errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("stale row: err = %v; want ErrVersionMismatch", err)
	}
}

// The seeder's Verify prepares every statement the layer registered,
// beside the data package's lock and blobfs's, and probes the read model's
// field contract, whose probes are statements of their own.
func TestStore_VerifyPreparesEveryStatement(t *testing.T) {
	s, db, rec := seederOver(t)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	registered := blobfsStatements(t)
	for _, entry := range db.Registry() {
		for _, st := range entry.Statements.Statements() {
			registered = append(registered, st.Text())
			if !slices.Contains(prepared, st.Text()) {
				t.Errorf("%s's statement %s was not verified", entry.Name, st.Name())
			}
		}
	}
	probes := slices.DeleteFunc(slices.Clone(prepared), func(sql string) bool {
		return slices.Contains(registered, sql)
	})
	if len(probes) == 0 {
		t.Error("Verify prepared the registered statements alone; want the read model's field contract probed")
	}
}

// transferTo decodes a transfer body the way the handler does, so the
// key-presence rule is exercised through the same path.
func transferTo(t *testing.T, body string) organization.TransferOrganization {
	t.Helper()
	var cmd organization.TransferOrganization
	if err := json.Unmarshal([]byte(body), &cmd); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// A list addressed by cursor continues past the previous page's last row
// with the keyset predicate, not by skipping rows, and is counted as the
// first page is.
func TestStore_ListContinuesByCursor(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	rows := func(codes ...string) sqltest.Response {
		r := sqltest.Response{Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"}}
		for i, code := range codes {
			id := fmt.Sprintf("00000000-0000-7000-8000-00000000000%d", i+1)
			r.Rows = append(r.Rows, []driver.Value{id, nil, code, code, int64(1), now, now, "/" + code})
		}
		return r
	}
	s, rec := service(t,
		sqltest.WithTotal(rows("a", "b"), 3), // page 1 of size 1 reads one row past
		sqltest.WithTotal(rows("b"), 3),      // continued: the last row, still counted
	)
	limits := web.Limits{DefaultSize: 1, MaxSize: 10, Cursor: true}

	q, _ := web.ParseQuery(url.Values{"size": {"1"}, "sort": {"code"}}, limits)
	first, paging, err := s.List(ctx, q)
	if err != nil || len(first) != 1 || (paging.Total == nil || *paging.Total != 3) || !paging.More || paging.Next == "" {
		t.Fatalf("first page = %v, %+v, %v", first, paging, err)
	}
	q, err = web.ParseQuery(url.Values{"size": {"1"}, "sort": {"code"}, "cursor": {paging.Next}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	next, paging, err := s.List(ctx, q)
	if err != nil || len(next) != 1 || next[0].Code != "b" || (paging.Total == nil || *paging.Total != 3) || paging.More {
		t.Fatalf("continued page = %v, %+v, %v", next, paging, err)
	}
	sqls := rec.SQL(sqltest.OpQuery)
	if !strings.Contains(sqls[1], "WHERE (q.code > CAST($1 AS text) OR (q.code = CAST($1 AS text) AND q.id > CAST($2 AS uuid)))") {
		t.Errorf("continued = %q; want the standard tier's keyset predicate past the cursor's row", sqls[1])
	}
}

// rowCount reads the rows a state carries under key, as a contribution
// reads them, and inserts nothing.
type rowCount struct {
	key  string
	rows []defaultRow
}

// defaultRow is one organization of the default state, as the state file
// spells it.
type defaultRow struct {
	Parent string `json:"parent"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

func (c *rowCount) Key() string { return c.key }
func (c *rowCount) Apply(_ context.Context, _ *sqlate.Tx, raw json.RawMessage) (int, error) {
	rows, err := data.SeedRows[defaultRow](raw)
	c.rows = rows
	return 0, err
}

// defaultOrganizations is the organizations the default state seeds, read
// from the state itself, so no test restates its inventory. The state
// lists them in dependency order, the root first.
func defaultOrganizations(t *testing.T) []defaultRow {
	t.Helper()
	pool, _ := sqltest.Open(t)
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), query.MustCatalog(query.Patterns(), data.Patterns()))
	c := &rowCount{key: "organizations"}
	if _, err := data.NewSeeder(db, c, noFiles("logos"), noFiles("documents")).Seed(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if len(c.rows) < 2 {
		t.Fatalf("the default state seeds %d organizations; the tests need a root and a child", len(c.rows))
	}
	return c.rows
}

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

// seeded scripts the seed's insert of the default state's rows from the
// first'th on, each under the id seededID gives it: the insert returning
// the id, the stored read for its event, and the event's outbox write.
func seeded(rows []defaultRow, first int) []sqltest.Response {
	ids := map[string]string{"": ""}
	for i, o := range rows {
		ids[o.Code] = seededID(i)
	}
	var out []sqltest.Response
	for i, o := range rows[first:] {
		id := seededID(first + i)
		if o.Parent == "" {
			out = append(out, idRow(id), stored(id, o.Code, o.Name, 1), written())
			continue
		}
		out = append(out, idRow(id), stored(id, o.Code, o.Name, 1, ids[o.Parent]), written())
	}
	return out
}

// seededID is the id the scripted seed gives the default state's i'th row.
func seededID(i int) string { return fmt.Sprintf("00000000-0000-7000-8000-%012x", 0x500+i) }

// wantCreated fails the test unless events are OrganizationCreated about
// each of the default state's rows from the first'th on, in file order,
// each carrying the data a create's carries, the stored row.
func wantCreated(t *testing.T, events []event.Event, rows []defaultRow, first int) {
	t.Helper()
	if len(events) != len(rows)-first {
		t.Fatalf("events committed = %d; want one per inserted organization, %d", len(events), len(rows)-first)
	}
	ids := map[string]any{"": nil}
	for i, o := range rows {
		ids[o.Code] = seededID(i)
	}
	for i, e := range events {
		o, id := rows[first+i], seededID(first+i)
		wantEvent(t, e, organization.OrganizationCreated.Type(), id,
			map[string]any{"organization_id": id, "parent_id": ids[o.Parent], "code": o.Code, "name": o.Name, "version": float64(1)})
	}
}

// The seed inserts every row of the state once, in one transaction, and
// each insert commits OrganizationCreated with it, its data the row read
// back as a create's is, its outbox write before the next row's insert.
func TestSeed_InsertsEveryRowOnce(t *testing.T) {
	rows := defaultOrganizations(t)
	s, rec := seeder(t, seeded(rows, 0)...)

	n, err := s.Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n["organizations"] != len(rows) {
		t.Fatalf("Seeded = %v; want %d organizations", n, len(rows))
	}
	want := []sqltest.Op{begin}
	for range rows {
		want = append(want, q, q, x)
	}
	sameOps(t, rec, append(want, commit)...)
	calls := rec.Calls()
	// The root binds a null parent; the second row binds the root's id.
	if calls[1].Args[0] != nil || calls[4].Args[0] != seededID(0) {
		t.Fatalf("parents bound as %v and %v; want nil then the root's id", calls[1].Args[0], calls[4].Args[0])
	}
	if !strings.Contains(calls[1].SQL, "ON CONFLICT ON CONSTRAINT uq_organization_parent_code DO NOTHING") {
		t.Fatalf("seed statement: %s", calls[1].SQL)
	}
	if calls[2].Args[0] != seededID(0) {
		t.Errorf("the stored read bound %v; want the inserted row's id", calls[2].Args)
	}
	wantCreated(t, committedEvents(t, rec), rows, 0)
}

// A row already there is found by its parent and code, and commits no
// event; the rows after it are inserted under it, each with its event.
func TestSeed_FindsExistingRows(t *testing.T) {
	rows := defaultOrganizations(t)
	// The root already exists: no row from the insert, then the lookup.
	responses := append([]sqltest.Response{{Columns: []string{"id"}}, idRow(seededID(0))}, seeded(rows, 1)...)
	s, rec := seeder(t, responses...)

	n, err := s.Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n["organizations"] != len(rows)-1 {
		t.Fatalf("inserted %d; want %d", n["organizations"], len(rows)-1)
	}
	calls := rec.Calls()
	if !strings.Contains(calls[2].SQL, "IS NOT DISTINCT FROM") || calls[3].Args[0] != seededID(0) {
		t.Fatalf("lookup did not resolve the root: %s %v", calls[2].SQL, calls[3].Args)
	}
	if rec.Pending() != 0 {
		t.Fatalf("pending %d", rec.Pending())
	}
	wantCreated(t, committedEvents(t, rec), rows, 1)
}

// A seed over a seeded state finds every row and commits no event.
func TestSeed_ASeededStateCommitsNoEvent(t *testing.T) {
	rows := defaultOrganizations(t)
	var responses []sqltest.Response
	for i := range rows {
		responses = append(responses, sqltest.Response{Columns: []string{"id"}}, idRow(seededID(i)))
	}
	s, rec := seeder(t, responses...)
	if n, err := s.Seed(context.Background(), "default"); err != nil || n["organizations"] != 0 {
		t.Fatalf("Seed = %v, %v; want nothing inserted", n, err)
	}
	if w := outboxWrites(rec); len(w) != 0 {
		t.Errorf("events written = %d; want none", len(w))
	}
}

// A failure rolls the transaction back, and with it the events of the
// rows inserted before it: no event commits.
func TestSeed_RollsBackOnFailure(t *testing.T) {
	rows := defaultOrganizations(t)
	s, rec := seeder(t, append(seeded(rows, 0)[:3], sqltest.Response{Err: errors.New("boom")})...)

	if _, err := s.Seed(context.Background(), "default"); err == nil || !strings.Contains(err.Error(), "seed organization "+rows[1].Code) {
		t.Fatalf("err = %v; want the failing row named", err)
	}
	sameOps(t, rec, begin, q, q, x, q, rollback)
	if len(outboxWrites(rec)) != 1 {
		t.Errorf("events written = %d; want the first row's, rolled back", len(outboxWrites(rec)))
	}
	noEventCommitted(t, rec)
}

// The layer's verifier covers its statements, the seed's among them, when
// the schema's startup runs the seeder's Verify.
func TestSeed_VerifiesTheLayersStatements(t *testing.T) {
	s, rec := seeder(t)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	var seed, lookup bool
	for _, sql := range rec.SQL(sqltest.OpPrepare) {
		seed = seed || strings.Contains(sql, "ON CONFLICT ON CONSTRAINT uq_organization_parent_code")
		lookup = lookup || strings.Contains(sql, "IS NOT DISTINCT FROM")
	}
	if !seed || !lookup {
		t.Fatalf("prepared %v; want the seed and its lookup among them", rec.SQL(sqltest.OpPrepare))
	}
}
