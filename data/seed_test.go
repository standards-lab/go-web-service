package data_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

// The seed file holds seven organizations in dependency order; the root
// is first.
const seedRows = 7

func newDatabase(t *testing.T, responses ...sqltest.Response) (*data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), data.Patterns())
	return data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog), rec
}

func idRow(id string) sqltest.Response {
	return sqltest.Response{Columns: []string{"id"}, Rows: [][]driver.Value{{id}}}
}

func TestSeeder_RegistersAndVerifies(t *testing.T) {
	db, rec := newDatabase(t)
	s := data.NewSeeder(db)

	reg := db.Registry()
	if len(reg) != 1 || reg[0].Name != "seed" || len(reg[0].Statements.Statements()) != 2 {
		t.Fatalf("registry = %+v; want the two seed statements under seed", reg)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := len(rec.SQL(sqltest.OpPrepare)); got != 2 {
		t.Fatalf("prepared %d statements; want 2", got)
	}
}

func TestSeeder_Seed_InsertsEveryRowOnce(t *testing.T) {
	responses := make([]sqltest.Response, 0, seedRows)
	for i := range seedRows {
		responses = append(responses, idRow(string(rune('a'+i))))
	}
	db, rec := newDatabase(t, responses...)
	s := data.NewSeeder(db)

	n, err := s.Seed(context.Background())
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if want := (admin.Seeded{"organizations": seedRows}); n["organizations"] != want["organizations"] {
		t.Fatalf("Seeded = %v; want %v", n, want)
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

func TestSeeder_Seed_FindsExistingRows(t *testing.T) {
	// The root already exists: no row from the insert, then the lookup.
	responses := []sqltest.Response{{Columns: []string{"id"}}, idRow("root")}
	for i := 1; i < seedRows; i++ {
		responses = append(responses, idRow(string(rune('a'+i))))
	}
	db, rec := newDatabase(t, responses...)
	s := data.NewSeeder(db)

	n, err := s.Seed(context.Background())
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

func TestSeeder_Seed_RollsBackOnFailure(t *testing.T) {
	db, rec := newDatabase(t, idRow("a"), sqltest.Response{Err: errBoom})
	s := data.NewSeeder(db)

	if _, err := s.Seed(context.Background()); err == nil || !strings.Contains(err.Error(), "seed organization engineering") {
		t.Fatalf("err = %v; want the failing row named", err)
	}
	ops := rec.Ops()
	if ops[len(ops)-1] != sqltest.OpRollback {
		t.Fatalf("ops = %v; want a rollback last", ops)
	}
}

var errBoom = errors.New("boom")
