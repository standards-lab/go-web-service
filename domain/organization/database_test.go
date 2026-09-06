package organization_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

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
	pool, rec := sqltest.Open(t, responses...)
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), query.MustCatalog(query.Patterns(), data.Patterns()))
	return organization.New(db), rec
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
		count(1), row(), // list
		row(),                         // find by id
		identity(validID, 1),          // create
		sqltest.Response{Affected: 1}, // edit
		sqltest.Response{Affected: 0}, // transfer: lock
		count(0),                      // transfer: in_subtree
		sqltest.Response{Affected: 1}, // transfer: update
		sqltest.Response{Affected: 1}, // delete
	)
	q, _ := web.ParseQuery(url.Values{"code": {"acme"}, "sort": {"-path"}}, web.Limits{DefaultSize: 20, MaxSize: 100})
	if items, total, err := s.List(ctx, q); err != nil || total != 1 || items[0].Path != "/acme" {
		t.Fatalf("List = %v, %d, %v", items, total, err)
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
	if !strings.HasPrefix(sqls[0], "SELECT COUNT(*) FROM (WITH RECURSIVE lineage") || !strings.Contains(sqls[1], "WHERE q.code = CAST($1 AS text) ORDER BY q.path DESC, q.id OFFSET $2") {
		t.Errorf("list = %q\n%q", sqls[0], sqls[1])
	}
	if create := sqls[3]; !strings.HasSuffix(create, "RETURNING id, version") {
		t.Errorf("create = %q; want the identity pattern spliced", create)
	}
	if edit := rec.SQL(sqltest.OpExec)[0]; edit != "UPDATE organization\nSET code = $1, name = $2, updated_at = CURRENT_TIMESTAMP, version = version + 1\nWHERE id = $3 AND version = $4" {
		t.Errorf("edit = %q", edit)
	}
	if lock := rec.Calls()[6]; lock.SQL != "SELECT pg_advisory_xact_lock(hashtext($1))" || lock.Args[0] != data.LockOrganizationTree {
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

// Verify prepares the seven statements and the read contract's probe.
func TestStore_VerifyPreparesEveryStatement(t *testing.T) {
	s, rec := service(t)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	if len(prepared) != 8 {
		t.Errorf("prepared %d statements, want 7 + the contract probe", len(prepared))
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
