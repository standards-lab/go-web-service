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
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

const (
	validID  = "00000000-0000-7000-8000-000000000000"
	parentID = "00000000-0000-7000-8000-000000000001"
)

func service(t *testing.T, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder) {
	t.Helper()
	svc, rec, _ := serviceOver(t, sqltest.Dialect{}, responses...)
	return svc, rec
}

func serviceOver(t *testing.T, dialect sqlate.Dialect, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	return serviceLogging(t, slog.New(slog.DiscardHandler), dialect, responses...)
}

func serviceLogging(t *testing.T, logger *slog.Logger, dialect sqlate.Dialect, responses ...sqltest.Response) (*organization.Service, *sqltest.Recorder, *storagetest.Fake) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, dialect, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fake := storagetest.NewFake(storagetest.Options{})
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

func recorder(t *testing.T) *organization.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return outboxtest.Stamped(ob.Sink())
}

func seeder(t *testing.T, responses ...sqltest.Response) (*data.Seeder, *sqltest.Recorder) {
	t.Helper()
	s, _, rec := seederOver(t, responses...)
	return s, rec
}

func seederOver(t *testing.T, responses ...sqltest.Response) (*data.Seeder, *data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{}, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog)
	svc := organization.New(db, data.NewStorage(db, fs, nil), slog.New(slog.DiscardHandler), recorder(t))
	return data.NewSeeder(db, svc.Seed(), noFiles("logos"), noFiles("documents")), db, rec
}

func blobfsStatements(t *testing.T) []string {
	t.Helper()
	fs, err := bfdata.New(query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns()), sqltest.Dialect{}, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pool, rec := sqltest.Open(t)
	if err := fs.Verify(context.Background(), sqlate.Wrap(pool, sqltest.Dialect{})); err != nil {
		t.Fatal(err)
	}
	return rec.SQL(sqltest.OpPrepare)
}

type noFiles string

func (k noFiles) Key() string { return string(k) }
func (noFiles) Write(context.Context, json.RawMessage, fs.FS) (int, error) {
	return 0, nil
}

func identity(id string, version int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"id", "version"}, Rows: [][]driver.Value{{id, version}}}
}

func subtreeCount(n int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"count"}, Rows: [][]driver.Value{{n}}}
}

func organizationFound() sqltest.Response {
	return sqltest.Response{Columns: []string{"version"}, Rows: [][]driver.Value{{int64(1)}}}
}

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

func eventWritten() sqltest.Response { return sqltest.Response{Affected: 1} }

func treeLocked() sqltest.Response { return exec(0) }

func row() sqltest.Response {
	now := time.Now()
	return sqltest.Response{
		Columns: []string{"id", "parent_id", "code", "name", "version", "created_at", "updated_at", "path"},
		Rows:    [][]driver.Value{{validID, nil, "acme", "Acme", int64(1), now, now, "/acme"}},
	}
}

func TestStore_EveryHandleBindsItsFilesParameters(t *testing.T) {
	ctx := context.Background()
	listed := []sqltest.Response{sqltest.WithTotal(row(), 1)}
	found := []sqltest.Response{row()}
	created := []sqltest.Response{identity(validID, 1), stored(validID, "eng", "Eng", 1), eventWritten()}
	edited := []sqltest.Response{exec(1), stored(validID, "eng", "Eng", 2), eventWritten()}
	transferred := []sqltest.Response{treeLocked(), subtreeCount(0), exec(1), stored(validID, "eng", "Eng", 3, parentID), eventWritten()}
	deleted := []sqltest.Response{exec(1), eventWritten()}
	s, rec := service(t, slices.Concat(listed, found, created, edited, transferred, deleted)...)
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
	createTx := []sqltest.Op{begin, q, q, x, commit}
	editTx := []sqltest.Op{begin, x, q, x, commit}
	transferTx := []sqltest.Op{begin, x, q, x, q, x, commit}
	deleteTx := []sqltest.Op{begin, x, x, commit}
	sameOps(t, rec, slices.Concat([]sqltest.Op{q, q}, createTx, editTx, transferTx, deleteTx)...)
}

func TestStore_TransferRejectsACycleBeforeTheUpdate(t *testing.T) {
	s, rec := service(t, treeLocked(), subtreeCount(1))
	_, err := s.Transfer(context.Background(), validID, 1, transferTo(t, `{"parent_id":"`+parentID+`"}`))
	if !errors.Is(err, organization.ErrCycle) {
		t.Fatalf("err = %v; want ErrCycle", err)
	}
	if ops := rec.Ops(); ops[len(ops)-1] != sqltest.OpRollback {
		t.Errorf("ops = %v; want a rollback", ops)
	}
}

func TestStore_TransferToRootBindsNull(t *testing.T) {
	s, rec := service(t, treeLocked(), exec(1), stored(validID, "eng", "Eng", 2), eventWritten())
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
	absent := []sqltest.Response{exec(0), {Columns: []string{"version"}}}
	stale := []sqltest.Response{exec(0), {Columns: []string{"version"}, Rows: [][]driver.Value{{int64(5)}}}}
	s, _ := service(t, slices.Concat(absent, stale)...)
	if _, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "a", Name: "A"}); err == nil || errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("absent row: err = %v; want the missing-row error", err)
	}
	if _, err := s.Edit(ctx, validID, 1, organization.EditOrganization{Code: "a", Name: "A"}); !errors.Is(err, query.ErrVersionMismatch) {
		t.Errorf("stale row: err = %v; want ErrVersionMismatch", err)
	}
}

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

func transferTo(t *testing.T, body string) organization.TransferOrganization {
	t.Helper()
	var cmd organization.TransferOrganization
	if err := json.Unmarshal([]byte(body), &cmd); err != nil {
		t.Fatal(err)
	}
	return cmd
}

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
	firstPageReadsOneRowPast := sqltest.WithTotal(rows("a", "b"), 3)
	continuedPageStillCounted := sqltest.WithTotal(rows("b"), 3)
	s, rec := service(t, firstPageReadsOneRowPast, continuedPageStillCounted)
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

type rowCount struct {
	key  string
	rows []defaultRow
}

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

func seeded(rows []defaultRow, first int) []sqltest.Response {
	ids := map[string]string{"": ""}
	for i, o := range rows {
		ids[o.Code] = seededID(i)
	}
	var out []sqltest.Response
	for i, o := range rows[first:] {
		id := seededID(first + i)
		if o.Parent == "" {
			out = append(out, idRow(id), stored(id, o.Code, o.Name, 1), eventWritten())
			continue
		}
		out = append(out, idRow(id), stored(id, o.Code, o.Name, 1, ids[o.Parent]), eventWritten())
	}
	return out
}

func seededID(i int) string { return fmt.Sprintf("00000000-0000-7000-8000-%012x", 0x500+i) }

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
		outboxtest.Want(t, e, organization.OrganizationCreated.Type(), id,
			map[string]any{"organization_id": id, "parent_id": ids[o.Parent], "code": o.Code, "name": o.Name, "version": float64(1)})
	}
}

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
	if calls[1].Args[0] != nil || calls[4].Args[0] != seededID(0) {
		t.Fatalf("parents bound as %v and %v; want nil then the root's id", calls[1].Args[0], calls[4].Args[0])
	}
	if !strings.Contains(calls[1].SQL, "ON CONFLICT ON CONSTRAINT uq_organization_parent_code DO NOTHING") {
		t.Fatalf("seed statement: %s", calls[1].SQL)
	}
	if calls[2].Args[0] != seededID(0) {
		t.Errorf("the stored read bound %v; want the inserted row's id", calls[2].Args)
	}
	wantCreated(t, outboxtest.Committed(t, rec), rows, 0)
}

func TestSeed_FindsExistingRows(t *testing.T) {
	rows := defaultOrganizations(t)
	rootInsertSkippedThenFound := []sqltest.Response{{Columns: []string{"id"}}, idRow(seededID(0))}
	responses := append(rootInsertSkippedThenFound, seeded(rows, 1)...)
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
	wantCreated(t, outboxtest.Committed(t, rec), rows, 1)
}

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
	if w := outboxtest.Writes(rec); len(w) != 0 {
		t.Errorf("events written = %d; want none", len(w))
	}
}

func TestSeed_RollsBackOnFailure(t *testing.T) {
	rows := defaultOrganizations(t)
	s, rec := seeder(t, append(seeded(rows, 0)[:3], sqltest.Response{Err: errors.New("boom")})...)

	if _, err := s.Seed(context.Background(), "default"); err == nil || !strings.Contains(err.Error(), "seed organization "+rows[1].Code) {
		t.Fatalf("err = %v; want the failing row named", err)
	}
	sameOps(t, rec, begin, q, q, x, q, rollback)
	if len(outboxtest.Writes(rec)) != 1 {
		t.Errorf("events written = %d; want the first row's, rolled back", len(outboxtest.Writes(rec)))
	}
	outboxtest.NoneCommitted(t, rec)
}

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
