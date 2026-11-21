package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"testing"

	"github.com/standards-lab/go-core/reactor"
	godb "github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-messaging/outbox"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/admin/database"
	"github.com/standards-lab/go-web-service/internal/outboxtest"
)

func recorder(t *testing.T) *database.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return outboxtest.Stamped(ob.Sink())
}

func TestOnReset_CommitsStateResetInItsOwnTransaction(t *testing.T) {
	pool, rec := sqltest.Open(t, sqltest.Response{Affected: 1})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(context.Background(), "empty"); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpExec, sqltest.OpCommit}) {
		t.Fatalf("ops = %v; want the outbox write alone in one transaction", ops)
	}
	e := outboxtest.Decode(t, rec.Calls()[1])
	outboxtest.WantEnvelope(t, e, "web-service.database.reset", "empty")
	if string(e.Data) != `"empty"` {
		t.Errorf("data = %s; want the state's name, \"empty\"", e.Data)
	}
}

func TestOnReset_AFailedWriteFailsTheHook(t *testing.T) {
	boom := errors.New("scripted: the outbox write failed")
	pool, rec := sqltest.Open(t, sqltest.Response{Err: boom})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(context.Background(), "default"); !errors.Is(err, boom) {
		t.Fatalf("hook = %v; want the write's error", err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpExec, sqltest.OpRollback}) {
		t.Errorf("ops = %v; want the write rolled back", ops)
	}
}

var oneTable = []migrate.Set{{Name: "app", Migrations: []migrate.Migration{
	{Version: 1, Name: "a", Up: "CREATE TABLE a (x int)", Down: "DROP TABLE a", Transactional: true},
}}}

var historyCols = []string{"version", "name", "dirty"}

func resetScript(write sqltest.Response) []sqltest.Response {
	return []sqltest.Response{
		{}, {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}, {}, {}, {},
		{}, {Columns: historyCols}, {}, {},
		write,
	}
}

type seedWatch struct {
	rec    *sqltest.Recorder
	seeded []int
}

func (s *seedWatch) Verify(context.Context) error { return nil }
func (s *seedWatch) States() []string             { return []string{"default", "empty"} }
func (s *seedWatch) Seed(context.Context, string) (admin.Seeded, error) {
	s.seeded = append(s.seeded, len(slices.DeleteFunc(s.rec.Calls(), func(c sqltest.Call) bool { return !outboxtest.IsWrite(c) })))
	return admin.Seeded{}, nil
}

func resetModule(t *testing.T, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder, *seedWatch) {
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
	m, err := migrate.New(sdb, oneTable, migrate.Options{Unlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	seed := &seedWatch{rec: rec}
	svc := admin.New(pdb, sdb, m, query.MustCatalog(query.Patterns()), admin.Options{
		Seed: "default", Seeder: seed, OnReset: database.OnReset(sdb, recorder(t)),
	})
	r := web.NewRouter()
	r.Mount(web.NewModule(database.Routes(svc, new(reactor.Gate), slog.New(slog.DiscardHandler))))
	return r, rec, seed
}

func TestState_CommitsStateResetBeforeTheSeed(t *testing.T) {
	status := []sqltest.Response{count(1), {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}}
	h, rec, seed := resetModule(t, slices.Concat(resetScript(sqltest.Response{Affected: 1}), status)...)

	decode(t, send(t, h, "POST", "/database/state", `{"state":"empty","confirm":true}`), http.StatusOK)
	var writes []sqltest.Call
	for _, c := range rec.Calls() {
		if outboxtest.IsWrite(c) {
			writes = append(writes, c)
		}
	}
	if len(writes) != 1 {
		t.Fatalf("outbox writes = %d; want the reset's one", len(writes))
	}
	if e := outboxtest.Decode(t, writes[0]); e.Type != database.StateReset.Type() || e.Subject != "empty" {
		t.Errorf("event = %s about %q; want StateReset about the state", e.Type, e.Subject)
	}
	if !slices.Equal(seed.seeded, []int{1}) {
		t.Errorf("seeds, with the outbox writes before each = %v; want one seed after the event", seed.seeded)
	}
}

func TestState_AFailedEventWriteStopsTheResetBeforeTheSeed(t *testing.T) {
	h, rec, seed := resetModule(t, resetScript(sqltest.Response{Err: errors.New("scripted: the outbox write failed")})...)

	if code := send(t, h, "POST", "/database/state", `{"state":"default","confirm":true}`).Code; code != http.StatusInternalServerError {
		t.Errorf("reset = %d; want the failed write's 500", code)
	}
	if len(seed.seeded) != 0 {
		t.Errorf("seeded after a failed event write: %v", seed.seeded)
	}
	calls := rec.Calls()
	i := slices.IndexFunc(calls, outboxtest.IsWrite)
	if i < 0 || i+1 >= len(calls) || calls[i+1].Op != sqltest.OpRollback {
		t.Errorf("ops = %v; want the event's write rolled back", rec.Ops())
	}
}
