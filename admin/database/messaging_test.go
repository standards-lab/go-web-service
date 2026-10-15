package database_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
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
)

// eventID and eventTime are what the tests' recorder stamps on the event
// it emits.
const eventID = "00000000-0000-7000-8000-0000000000ee"

var eventTime = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// recorder is the recorder the composition root injects, go-messaging's
// over its Postgres outbox, with the id and the time it stamps fixed: its
// event's write is one statement, which the scripted driver records.
func recorder(t *testing.T) *database.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return event.NewRecorder(ob.Sink(), "web-service",
		event.IDs(func() string { return eventID }), event.Clock(func() time.Time { return eventTime }))
}

// bytesOf is a bound argument's bytes, as the driver was handed them.
func bytesOf(t *testing.T, arg any) []byte {
	t.Helper()
	switch v := arg.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case json.RawMessage:
		return v
	}
	t.Fatalf("argument %T is neither bytes nor a string", arg)
	return nil
}

// isOutboxWrite reports whether c is the outbox's insert.
func isOutboxWrite(c sqltest.Call) bool {
	return c.Op == sqltest.OpExec && strings.Contains(c.SQL, "messaging_outbox")
}

// writtenEvent decodes the outbox insert's event: its parameters are the
// source, the id, the header as JSON, and the data, in that order.
func writtenEvent(t *testing.T, c sqltest.Call) event.Event {
	t.Helper()
	if len(c.Args) != 4 {
		t.Fatalf("outbox insert args = %v; want source, id, header, data", c.Args)
	}
	var h event.Header
	if err := json.Unmarshal(bytesOf(t, c.Args[2]), &h); err != nil {
		t.Fatalf("decode the header: %v", err)
	}
	e, err := event.Decode(h, bytesOf(t, c.Args[3]))
	if err != nil {
		t.Fatalf("decode the event: %v", err)
	}
	return e
}

// The hook commits StateReset in a transaction of its own that holds the
// outbox write alone: its subject is the state's name, and its data the
// state's name under "state", the only field.
func TestOnReset_CommitsStateResetInItsOwnTransaction(t *testing.T) {
	pool, rec := sqltest.Open(t, sqltest.Response{Affected: 1})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(context.Background(), "empty"); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpExec, sqltest.OpCommit}) {
		t.Fatalf("ops = %v; want the outbox write alone in one transaction", ops)
	}
	e := writtenEvent(t, rec.Calls()[1])
	if e.Type != database.StateReset.Type() || e.Type != "web-service.database.reset" || e.Subject != "empty" {
		t.Errorf("event = %s about %q; want web-service.database.reset about the state", e.Type, e.Subject)
	}
	if e.ID != eventID || e.Source != "web-service" || !e.Time.Equal(eventTime) || e.DataContentType != "application/json" {
		t.Errorf("envelope = %+v; want the recorder's stamp and JSON data", e)
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("decode the data: %v", err)
	}
	if len(data) != 1 || data["state"] != "empty" {
		t.Errorf("data = %v; want the state's name alone", data)
	}
}

// A refused outbox write fails the hook and rolls its transaction back.
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

// oneTable is a one-migration set, so a scripted reset's revert and apply
// stay a few statements; the hook is indifferent to what the sets hold.
var oneTable = []migrate.Set{{Name: "app", Migrations: []migrate.Migration{
	{Version: 1, Name: "a", Up: "CREATE TABLE a (x int)", Down: "DROP TABLE a", Transactional: true},
}}}

var historyCols = []string{"version", "name", "dirty"}

// resetScript is the scripted driver's side of a reset over oneTable,
// unlocked: the revert (the history table, the applied history, the
// migration's down and its history delete, the history table's drop), the
// apply (the history table, the empty history, the up and its history
// insert), then the hook's outbox write, answering write.
func resetScript(write sqltest.Response) []sqltest.Response {
	return []sqltest.Response{
		{}, {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}, {}, {}, {},
		{}, {Columns: historyCols}, {}, {},
		write,
	}
}

// seedWatch is a seeder that records, at each seed, how many outbox writes
// the reset had made.
type seedWatch struct {
	rec    *sqltest.Recorder
	seeded []int
}

func (s *seedWatch) Verify(context.Context) error { return nil }
func (s *seedWatch) States() []string             { return []string{"default", "empty"} }
func (s *seedWatch) Seed(context.Context, string) (admin.Seeded, error) {
	s.seeded = append(s.seeded, len(slices.DeleteFunc(s.rec.Calls(), func(c sqltest.Call) bool { return !isOutboxWrite(c) })))
	return admin.Seeded{}, nil
}

// resetModule composes the admin domain with the reset hook wired as the
// composition root wires it, over oneTable and seed.
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

// A state reset commits StateReset once, about the state it applied,
// after the revert and the apply and before the seed, which finds it
// written. Nothing else the reset runs writes the outbox.
func TestState_CommitsStateResetBeforeTheSeed(t *testing.T) {
	status := []sqltest.Response{count(1), {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}}
	h, rec, seed := resetModule(t, slices.Concat(resetScript(sqltest.Response{Affected: 1}), status)...)

	decode(t, send(t, h, "POST", "/database/state", `{"state":"empty","confirm":true}`), http.StatusOK)
	var writes []sqltest.Call
	for _, c := range rec.Calls() {
		if isOutboxWrite(c) {
			writes = append(writes, c)
		}
	}
	if len(writes) != 1 {
		t.Fatalf("outbox writes = %d; want the reset's one", len(writes))
	}
	if e := writtenEvent(t, writes[0]); e.Type != database.StateReset.Type() || e.Subject != "empty" {
		t.Errorf("event = %s about %q; want StateReset about the state", e.Type, e.Subject)
	}
	if !slices.Equal(seed.seeded, []int{1}) {
		t.Errorf("seeds, with the outbox writes before each = %v; want one seed after the event", seed.seeded)
	}
}

// A refused event write fails the reset before it seeds: the answer is
// the failure's 500, and the seeder never runs.
func TestState_AFailedEventWriteStopsTheResetBeforeTheSeed(t *testing.T) {
	h, rec, seed := resetModule(t, resetScript(sqltest.Response{Err: errors.New("scripted: the outbox write failed")})...)

	if code := send(t, h, "POST", "/database/state", `{"state":"default","confirm":true}`).Code; code != http.StatusInternalServerError {
		t.Errorf("reset = %d; want the failed write's 500", code)
	}
	if len(seed.seeded) != 0 {
		t.Errorf("seeded after a failed event write: %v", seed.seeded)
	}
	calls := rec.Calls()
	i := slices.IndexFunc(calls, isOutboxWrite)
	if i < 0 || i+1 >= len(calls) || calls[i+1].Op != sqltest.OpRollback {
		t.Errorf("ops = %v; want the event's write rolled back", rec.Ops())
	}
}
