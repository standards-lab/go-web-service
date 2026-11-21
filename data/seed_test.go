package data_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

func newDatabase(t *testing.T, responses ...sqltest.Response) (*data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	catalog := query.MustCatalog(query.Patterns(), data.Patterns())
	return data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog), rec
}

type contribution struct {
	key   string
	order *[]string
	rec   *sqltest.Recorder
	rows  []json.RawMessage
	err   error
}

func (c *contribution) Key() string { return c.key }

func (c *contribution) Apply(ctx context.Context, tx *sqlate.Tx, raw json.RawMessage) (int, error) {
	*c.order = append(*c.order, c.key)
	rows, err := data.SeedRows[json.RawMessage](raw)
	if err != nil {
		return 0, err
	}
	c.rows = rows
	c.rec.Queue(make([]sqltest.Response, len(rows))...)
	for range rows {
		if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
			return 0, err
		}
	}
	return len(rows), c.err
}

type fileContribution struct {
	key      string
	order    *[]string
	rec      *sqltest.Recorder
	rows     []json.RawMessage
	opsAtRun []sqltest.Op
	fixture  []byte
	err      error
}

func (c *fileContribution) Key() string { return c.key }

type verifier struct {
	ran bool
	err error
}

func (v *verifier) Verify(context.Context, sqlate.Session) error {
	v.ran = true
	return v.err
}

func (c *fileContribution) Write(_ context.Context, raw json.RawMessage, fixtures fs.FS) (int, error) {
	*c.order = append(*c.order, c.key)
	c.opsAtRun = c.rec.Ops()
	rows, err := data.SeedRows[json.RawMessage](raw)
	if err != nil {
		return 0, err
	}
	c.rows = rows
	if c.fixture, err = fs.ReadFile(fixtures, "acme.png"); err != nil {
		return 0, err
	}
	return len(rows), c.err
}

var widgets = fstest.MapFS{
	"statements/widget.sql": {Data: []byte("--| tier: standard\n-- A widget store's read.\nSELECT 1\n")},
}

func TestSeeder_VerifiesEveryRegisteredStore(t *testing.T) {
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	fs, err := bfdata.New(catalog, sqltest.Dialect{}, bfdata.Options{})
	if err != nil {
		t.Fatal(err)
	}
	alone, blobfsRec := sqltest.Open(t)
	if err := fs.Verify(context.Background(), sqlate.Wrap(alone, sqltest.Dialect{})); err != nil {
		t.Fatal(err)
	}
	pool, rec := sqltest.Open(t)
	db := data.New(sqlate.Wrap(pool, sqltest.Dialect{}), catalog)
	s := data.NewSeeder(db)
	stmts := db.Catalog.MustCompile(widgets, "statements", db.Dialect())
	db.Register("widget", stmts, stmts)
	data.NewStorage(db, fs, nil)

	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	for _, entry := range db.Registry() {
		for _, st := range entry.Statements.Statements() {
			if !slices.Contains(prepared, st.Text()) {
				t.Errorf("%s's statement %s was not verified", entry.Name, st.Name())
			}
		}
	}
	blobfs := blobfsRec.SQL(sqltest.OpPrepare)
	if len(blobfs) == 0 {
		t.Fatal("blobfs's store prepared nothing; want its statements")
	}
	for _, sql := range blobfs {
		if !slices.Contains(prepared, sql) {
			t.Errorf("blobfs's statement %q was not verified", sql)
		}
	}
}

func TestSeeder_VerifyReportsARegisteredStoresFailure(t *testing.T) {
	db, _ := newDatabase(t)
	store := &verifier{err: errBoom}
	db.Register("widget", db.Catalog.MustCompile(widgets, "statements", db.Dialect()), store)

	if err := data.NewSeeder(db).Verify(context.Background()); !store.ran || !errors.Is(err, errBoom) {
		t.Fatalf("Verify = %v, store ran %t; want the store's failure", err, store.ran)
	}
}

func TestSeeder_VerifyRunsARecordedVerifier(t *testing.T) {
	db, _ := newDatabase(t)
	entries := len(db.Registry())
	store := &verifier{err: errBoom}
	db.Record(store)

	if err := data.NewSeeder(db).Verify(context.Background()); !store.ran || !errors.Is(err, errBoom) {
		t.Fatalf("Verify = %v, store ran %t; want the recorded store's failure", err, store.ran)
	}
	if n := len(db.Registry()); n != entries {
		t.Errorf("registry holds %d entries after Record, want %d", n, entries)
	}
}

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

func TestNewSeeder_PanicsOnAContributionOfNoOneKind(t *testing.T) {
	db, _ := newDatabase(t)
	cases := map[string]data.Contribution{
		"neither": neither{},
		"both":    both{},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), name) {
					t.Fatalf("NewSeeder recovered %v; want the panic to say %s", r, name)
				}
			}()
			data.NewSeeder(db, c)
		})
	}
}

type neither struct{}

func (neither) Key() string { return "x" }

type both struct{ neither }

func (both) Apply(context.Context, *sqlate.Tx, json.RawMessage) (int, error) { return 0, nil }
func (both) Write(context.Context, json.RawMessage, fs.FS) (int, error)      { return 0, nil }

func TestSeeder_States_ListsTheFiles(t *testing.T) {
	db, _ := newDatabase(t)
	s := data.NewSeeder(db)
	got := s.States()
	if !slices.IsSorted(got) || !slices.Contains(got, "default") || !slices.Contains(got, "empty") {
		t.Fatalf("States = %v; want the files sorted, default and empty among them", got)
	}
	for _, name := range got {
		if _, err := s.Seed(context.Background(), name); errors.Is(err, admin.ErrUnknownState) {
			t.Errorf("Seed(%q) = %v; want a listed state known", name, err)
		}
	}
}

func TestSeeder_Seed_EmptyStateInsertsNothing(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	orgs := &contribution{key: "organizations", order: &order}
	logos := &fileContribution{key: "logos", order: &order, rec: rec}
	n, err := data.NewSeeder(db, orgs, logos).Seed(context.Background(), "empty")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if v, ok := n["organizations"]; !ok || v != 0 || n["logos"] != 0 || len(n) != 2 || len(order) != 0 {
		t.Fatalf("Seeded = %v, applied %v; want both at zero, nothing applied", n, order)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpCommit}) {
		t.Fatalf("ops = %v; want an empty transaction", ops)
	}
}

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

func TestSeeder_Seed_AnUnreadKeyIsRefused(t *testing.T) {
	db, rec := newDatabase(t)
	_, err := data.NewSeeder(db).Seed(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), `"documents"`) {
		t.Fatalf("err = %v; want the first unread key named", err)
	}
	if len(rec.Calls()) != 0 {
		t.Fatalf("a refused state reached the database: %v", rec.Ops())
	}
}

func defaultContributions(order *[]string, rec *sqltest.Recorder) (*contribution, *fileContribution, *fileContribution) {
	return &contribution{key: "organizations", order: order, rec: rec},
		&fileContribution{key: "logos", order: order, rec: rec},
		&fileContribution{key: "documents", order: order, rec: rec}
}

func TestSeeder_Seed_AppliesRowsThenFilesInOrder(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	orgs, logos, docs := defaultContributions(&order, rec)
	absent := &contribution{key: "people", order: &order}
	filesGivenBeforeRows := data.NewSeeder(db, logos, docs, orgs, absent)
	n, err := filesGivenBeforeRows.Seed(context.Background(), "default")
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(orgs.rows) == 0 || len(logos.rows) == 0 || len(docs.rows) == 0 {
		t.Fatalf("handed %d organizations, %d logos, %d trees; want the default state's rows under each key", len(orgs.rows), len(logos.rows), len(docs.rows))
	}
	if n["organizations"] != len(orgs.rows) || n["logos"] != len(logos.rows) || n["documents"] != len(docs.rows) || n["people"] != 0 || len(n) != 4 {
		t.Fatalf("Seeded = %v; want each contribution's count, people at zero", n)
	}
	if !slices.Equal(order, []string{"organizations", "logos", "documents"}) {
		t.Fatalf("applied %v; want the rows, then logos, then documents", order)
	}
	for _, f := range []*fileContribution{logos, docs} {
		if last := f.opsAtRun[len(f.opsAtRun)-1]; last != sqltest.OpCommit {
			t.Errorf("%s ran at ops %v; want the row transaction committed", f.key, f.opsAtRun)
		}
		if !bytes.HasPrefix(f.fixture, []byte("\x89PNG")) {
			t.Errorf("%s read a fixture of %d bytes; want the embedded PNG", f.key, len(f.fixture))
		}
	}
	ops := rec.Ops()
	if ops[0] != sqltest.OpBegin || ops[len(ops)-1] != sqltest.OpCommit || len(ops) != len(orgs.rows)+2 {
		t.Fatalf("ops = %v; want every row in one transaction", ops)
	}
}

func TestSeeder_Seed_RollsBackOnFailure(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	orgs, logos, docs := defaultContributions(&order, rec)
	orgs.err = errBoom
	if _, err := data.NewSeeder(db, orgs, logos, docs).Seed(context.Background(), "default"); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v; want the contribution's failure", err)
	}
	ops := rec.Ops()
	if ops[len(ops)-1] != sqltest.OpRollback || !slices.Equal(order, []string{"organizations"}) {
		t.Fatalf("ops = %v, applied %v; want a rollback last and no file written", ops, order)
	}
}

func TestSeeder_Seed_AFailedFileWriteStopsAfterTheCommit(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	orgs, logos, docs := defaultContributions(&order, rec)
	logos.err = errBoom
	n, err := data.NewSeeder(db, orgs, logos, docs).Seed(context.Background(), "default")
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "logos") {
		t.Fatalf("err = %v; want the logos' failure, named", err)
	}
	if n["organizations"] != len(orgs.rows) || !slices.Equal(order, []string{"organizations", "logos"}) {
		t.Fatalf("Seeded = %v, applied %v; want the rows counted and documents not run", n, order)
	}
	if ops := rec.Ops(); ops[len(ops)-1] != sqltest.OpCommit {
		t.Fatalf("ops = %v; want the rows committed", ops)
	}
}

func TestSeedFixtures_AreLogosTheUploadAccepts(t *testing.T) {
	db, rec := newDatabase(t)
	var order []string
	orgs, _, docs := defaultContributions(&order, rec)
	logos := &fixtureReader{}
	if _, err := data.NewSeeder(db, orgs, logos, docs).Seed(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if len(logos.sizes) == 0 || len(logos.sizes) != len(orgs.rows) {
		t.Fatalf("read %d fixtures; want one for each of the %d organizations", len(logos.sizes), len(orgs.rows))
	}
	for name, size := range logos.sizes {
		if size > 1<<20 {
			t.Errorf("%s is %d bytes, over the logo's 1 MiB", name, size)
		}
	}
}

type fixtureReader struct{ sizes map[string]int }

func (*fixtureReader) Key() string { return "logos" }
func (f *fixtureReader) Write(_ context.Context, raw json.RawMessage, fixtures fs.FS) (int, error) {
	type row struct {
		Organization string `json:"organization"`
		ID           string `json:"id"`
		Fixture      string `json:"fixture"`
	}
	rows, err := data.SeedRows[row](raw)
	if err != nil {
		return 0, err
	}
	f.sizes = make(map[string]int, len(rows))
	for _, r := range rows {
		b, err := fs.ReadFile(fixtures, r.Fixture)
		if err != nil {
			return 0, err
		}
		if http.DetectContentType(b) != "image/png" {
			return 0, fmt.Errorf("%s sniffs as %s", r.Fixture, http.DetectContentType(b))
		}
		if _, err := png.Decode(bytes.NewReader(b)); err != nil {
			return 0, fmt.Errorf("%s: %w", r.Fixture, err)
		}
		f.sizes[r.Fixture] = len(b)
	}
	return len(rows), nil
}

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
	}, nil)
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
