package organization_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
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
	return organization.New(db, data.NewStorage(fs, objects)), rec, fake
}

// seeder composes the data package's seeder over the layer's seed
// contribution, as the composition root does.
func seeder(t *testing.T, responses ...sqltest.Response) (*data.Seeder, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog)
	svc := organization.New(db, data.NewStorage(fs, nil))
	return data.NewSeeder(db, svc.Seed()), rec
}

func identity(id string, version int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"id", "version"}, Rows: [][]driver.Value{{id, version}}}
}

func count(n int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"count"}, Rows: [][]driver.Value{{n}}}
}

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
		sqltest.WithTotal(row(), 1),   // list: the page and its window count in one statement
		row(),                         // find by id
		identity(validID, 1),          // create
		sqltest.Response{Affected: 1}, // edit
		sqltest.Response{Affected: 0}, // transfer: lock
		count(0),                      // transfer: in_subtree
		sqltest.Response{Affected: 1}, // transfer: update
		sqltest.Response{Affected: 1}, // delete
	)
	q, _ := web.ParseQuery(url.Values{"code": {"acme"}, "sort": {"-path"}}, web.Limits{DefaultSize: 20, MaxSize: 100})
	if items, paging, err := s.List(ctx, q); err != nil || paging.Total != 1 || paging.More || items[0].Path != "/acme" {
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
	if edit := rec.SQL(sqltest.OpExec)[0]; edit != "UPDATE organization\nSET code = $1, name = $2, updated_at = CURRENT_TIMESTAMP, version = version + 1\nWHERE id = $3 AND version = $4" {
		t.Errorf("edit = %q", edit)
	}
	if lock := rec.Calls()[5]; lock.SQL != "SELECT pg_advisory_xact_lock(hashtext($1))" || lock.Args[0] != data.LockOrganizationTree {
		t.Errorf("transfer did not take the named tree lock first: %+v", lock)
	}
	ops := rec.Ops()
	if ops[len(ops)-1] != sqltest.OpExec || ops[len(ops)-2] != sqltest.OpCommit {
		t.Errorf("ops = %v, want the transfer committed and the delete last", ops)
	}
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
	s, rec := service(t, sqltest.Response{Affected: 0}, sqltest.Response{Affected: 1})
	if _, err := s.Transfer(context.Background(), validID, 1, transferTo(t, `{"parent_id":null}`)); err != nil {
		t.Fatal(err)
	}
	update := rec.Calls()[2]
	if update.Args[0] != nil || len(rec.SQL(sqltest.OpQuery)) != 0 {
		t.Errorf("root transfer: args %v, queries %v; want a null parent and no cycle walk", update.Args, rec.SQL(sqltest.OpQuery))
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

// Verify prepares the twelve statements and the read contract's three
// probes: the fields against their declared types, a page past a cursor,
// and the same page counted.
func TestStore_VerifyPreparesEveryStatement(t *testing.T) {
	s, rec := service(t)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	if len(prepared) != 15 {
		t.Errorf("prepared %d statements, want 12 + the contract's 3 probes", len(prepared))
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
	if err != nil || len(first) != 1 || paging.Total != 3 || !paging.More || paging.Next == "" {
		t.Fatalf("first page = %v, %+v, %v", first, paging, err)
	}
	q, err = web.ParseQuery(url.Values{"size": {"1"}, "sort": {"code"}, "cursor": {paging.Next}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	next, paging, err := s.List(ctx, q)
	if err != nil || len(next) != 1 || next[0].Code != "b" || paging.Total != 3 || paging.More {
		t.Fatalf("continued page = %v, %+v, %v", next, paging, err)
	}
	sqls := rec.SQL(sqltest.OpQuery)
	if !strings.Contains(sqls[1], "WHERE (q.code > CAST($1 AS text) OR (q.code = CAST($1 AS text) AND q.id > CAST($2 AS uuid)))") {
		t.Errorf("continued = %q; want the standard tier's keyset predicate past the cursor's row", sqls[1])
	}
}

// The default state holds seven organizations in dependency order; the
// root is first.
const seedRows = 7

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

func TestSeed_InsertsEveryRowOnce(t *testing.T) {
	responses := make([]sqltest.Response, 0, seedRows)
	for i := range seedRows {
		responses = append(responses, idRow(string(rune('a'+i))))
	}
	s, rec := seeder(t, responses...)

	n, err := s.Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n["organizations"] != seedRows {
		t.Fatalf("Seeded = %v; want %d organizations", n, seedRows)
	}
	ops := rec.Ops()
	if ops[0] != sqltest.OpBegin || ops[len(ops)-1] != sqltest.OpCommit {
		t.Fatalf("ops = %v; want one transaction", ops)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Fatalf("pending %d, leaked %d", rec.Pending(), rec.RowsLeaked())
	}
	calls := rec.Calls()
	// The root binds a null parent; the second row binds the root's id.
	if calls[1].Args[0] != nil || calls[2].Args[0] != "a" {
		t.Fatalf("parents bound as %v and %v; want nil then a", calls[1].Args[0], calls[2].Args[0])
	}
	if !strings.Contains(calls[1].SQL, "ON CONFLICT ON CONSTRAINT uq_organization_parent_code DO NOTHING") {
		t.Fatalf("seed statement: %s", calls[1].SQL)
	}
}

func TestSeed_FindsExistingRows(t *testing.T) {
	// The root already exists: no row from the insert, then the lookup.
	responses := []sqltest.Response{{Columns: []string{"id"}}, idRow("root")}
	for i := 1; i < seedRows; i++ {
		responses = append(responses, idRow(string(rune('a'+i))))
	}
	s, rec := seeder(t, responses...)

	n, err := s.Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n["organizations"] != seedRows-1 {
		t.Fatalf("inserted %d; want %d", n["organizations"], seedRows-1)
	}
	calls := rec.Calls()
	if !strings.Contains(calls[2].SQL, "IS NOT DISTINCT FROM") || calls[3].Args[0] != "root" {
		t.Fatalf("lookup did not resolve the root: %s %v", calls[2].SQL, calls[3].Args)
	}
	if rec.Pending() != 0 {
		t.Fatalf("pending %d", rec.Pending())
	}
}

func TestSeed_RollsBackOnFailure(t *testing.T) {
	s, rec := seeder(t, idRow("a"), sqltest.Response{Err: errors.New("boom")})

	if _, err := s.Seed(context.Background(), "default"); err == nil || !strings.Contains(err.Error(), "seed organization engineering") {
		t.Fatalf("err = %v; want the failing row named", err)
	}
	ops := rec.Ops()
	if ops[len(ops)-1] != sqltest.OpRollback {
		t.Fatalf("ops = %v; want a rollback last", ops)
	}
}

// The contribution verifies the layer's statements, the seed's among
// them, at the schema stage the seeder's Verify runs at.
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
