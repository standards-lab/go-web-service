package data_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

// The default state holds seven organizations.
const seedRows = 7

func newDatabase(t *testing.T, responses ...sqltest.Response) (*data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), data.Patterns())
	return data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog), rec
}

// contribution is a domain's seed contribution as the seeder sees it: it
// records the rows it was handed, in the order the seeder applied it, and
// reports each row inserted, running one statement per row so the
// transaction's shape shows, unless err refuses the apply.
type contribution struct {
	key      string
	order    *[]string
	rows     []json.RawMessage
	verified bool
	err      error
}

func (c *contribution) Key() string { return c.key }

func (c *contribution) Verify(context.Context) error {
	c.verified = true
	return c.err
}

func (c *contribution) Apply(ctx context.Context, tx *sqlate.Tx, raw json.RawMessage) (int, error) {
	*c.order = append(*c.order, c.key)
	rows, err := data.SeedRows[json.RawMessage](raw)
	if err != nil {
		return 0, err
	}
	c.rows = rows
	for range rows {
		if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
			return 0, err
		}
	}
	return len(rows), c.err
}

func TestSeeder_VerifiesItsOwnAndEveryContributions(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	c := &contribution{key: "organizations", order: &order}
	s := data.NewSeeder(db, c)

	reg := db.Registry()
	if len(reg) != 1 || reg[0].Name != "data" || len(reg[0].Statements.Statements()) != 1 {
		t.Fatalf("registry = %+v; want the lock alone under data", reg)
	}
	if err := s.Verify(context.Background()); err != nil || !c.verified {
		t.Fatalf("Verify: %v, contribution verified %v", err, c.verified)
	}
	if got := len(rec.SQL(sqltest.OpPrepare)); got != 1 {
		t.Fatalf("prepared %d statements; want the lock", got)
	}
	c.err = errBoom
	if err := s.Verify(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("Verify = %v; want the contribution's failure", err)
	}
}

// Two contributions under one key are a wiring defect.
func TestNewSeeder_PanicsOnADuplicateKey(t *testing.T) {
	db, _ := newDatabase(t)
	defer func() {
		if recover() == nil {
			t.Fatal("NewSeeder accepted two contributions under one key")
		}
	}()
	var order []string
	data.NewSeeder(db, &contribution{key: "a", order: &order}, &contribution{key: "a", order: &order})
}

// The states are the embedded files, by name, sorted.
func TestSeeder_States_ListsTheFiles(t *testing.T) {
	db, _ := newDatabase(t)
	if got := data.NewSeeder(db).States(); !slices.Equal(got, []string{"default", "empty"}) {
		t.Fatalf("States = %v; want default and empty", got)
	}
}

// The empty state applies no contribution and still reports every one, at
// zero, in one transaction.
func TestSeeder_Seed_EmptyStateInsertsNothing(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	n, err := data.NewSeeder(db, &contribution{key: "organizations", order: &order}).Seed(context.Background(), "empty")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if v, ok := n["organizations"]; !ok || v != 0 || len(order) != 0 {
		t.Fatalf("Seeded = %v, applied %v; want organizations at zero, nothing applied", n, order)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpCommit}) {
		t.Fatalf("ops = %v; want an empty transaction", ops)
	}
}

// A name with no file is the admin service's unknown-state error, before
// any I/O.
func TestSeeder_Seed_UnknownStateIsRefused(t *testing.T) {
	db, rec := newDatabase(t)
	_, err := data.NewSeeder(db).Seed(context.Background(), "nope")
	if !errors.Is(err, admin.ErrUnknownState) || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v; want ErrUnknownState naming it", err)
	}
	if len(rec.Calls()) != 0 {
		t.Fatalf("an unknown state reached the database: %v", rec.Ops())
	}
}

// A key no contribution reads is a defect in the file, refused before any
// I/O: the default state's organizations with no contribution for them.
func TestSeeder_Seed_AnUnreadKeyIsRefused(t *testing.T) {
	db, rec := newDatabase(t)
	_, err := data.NewSeeder(db).Seed(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), `"organizations"`) {
		t.Fatalf("err = %v; want the unread key named", err)
	}
	if len(rec.Calls()) != 0 {
		t.Fatalf("a refused state reached the database: %v", rec.Ops())
	}
}

// Every contribution applies in the order given, in one transaction, and
// the counts carry each one's key; one the state does not carry reports
// zero.
func TestSeeder_Seed_AppliesEveryContributionInOrder(t *testing.T) {
	responses := make([]sqltest.Response, seedRows)
	db, rec := newDatabase(t, responses...)
	var order []string
	orgs := &contribution{key: "organizations", order: &order}
	later := &contribution{key: "documents", order: &order}
	n, err := data.NewSeeder(db, orgs, later).Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if n["organizations"] != seedRows || n["documents"] != 0 || len(n) != 2 {
		t.Fatalf("Seeded = %v; want %d organizations and documents at zero", n, seedRows)
	}
	if !slices.Equal(order, []string{"organizations"}) || len(orgs.rows) != seedRows {
		t.Fatalf("applied %v with %d rows; want organizations' %d rows alone", order, len(orgs.rows), seedRows)
	}
	ops := rec.Ops()
	if ops[0] != sqltest.OpBegin || ops[len(ops)-1] != sqltest.OpCommit || len(ops) != seedRows+2 {
		t.Fatalf("ops = %v; want every row in one transaction", ops)
	}
}

func TestSeeder_Seed_RollsBackOnFailure(t *testing.T) {
	db, rec := newDatabase(t, make([]sqltest.Response, seedRows)...)
	var order []string
	s := data.NewSeeder(db, &contribution{key: "organizations", order: &order, err: errBoom})
	if _, err := s.Seed(context.Background(), "default"); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v; want the contribution's failure", err)
	}
	ops := rec.Ops()
	if ops[len(ops)-1] != sqltest.OpRollback {
		t.Fatalf("ops = %v; want a rollback last", ops)
	}
}

// SeedRows decodes strictly: a field the row type does not carry is a
// defect in the file.
func TestSeedRows_RefusesAnUnknownField(t *testing.T) {
	type row struct {
		Code string `json:"code"`
	}
	rows, err := data.SeedRows[row](json.RawMessage(`[{"code":"acme"}]`))
	if err != nil || len(rows) != 1 || rows[0].Code != "acme" {
		t.Fatalf("SeedRows = %+v, %v", rows, err)
	}
	if _, err := data.SeedRows[row](json.RawMessage(`[{"code":"acme","parent":""}]`)); err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("err = %v; want the unknown field named", err)
	}
}

var errBoom = errors.New("boom")

func TestLock_TakesTheNamedLockInsideTheTransaction(t *testing.T) {
	db, rec := newDatabase(t, sqltest.Response{Affected: 0})
	_, err := db.Transact(context.Background(), func(tx *sqlate.Tx) (struct{}, error) {
		return struct{}{}, db.Lock(context.Background(), tx, data.LockOrganizationTree)
	})
	if err != nil {
		t.Fatal(err)
	}
	if lock := rec.Calls()[1]; lock.SQL != "SELECT pg_advisory_xact_lock(hashtext($1))" || lock.Args[0] != "organization.tree" {
		t.Fatalf("lock call = %+v", lock)
	}
	if err := db.Lock(context.Background(), db.DB, data.LockOrganizationTree); !errors.Is(err, query.ErrTransactionRequired) {
		t.Fatalf("outside a transaction: err = %v; want ErrTransactionRequired", err)
	}
}
