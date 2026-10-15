//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-messaging/outbox"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/integration"
)

// probeData is the payload of the suite's probe event.
type probeData struct {
	Note string `json:"note"`
}

// probe is the suite's own event kind, under the service's prefix so the
// broker accepts it. The suite is its producer, beneath the API: emitProbe
// commits an event through go-messaging's outbox, the path a domain's
// command takes, at a moment no request could, and the running service's
// relay publishes it as it would a domain's.
var probe = event.Define[probeData]("web-service.integration.probe")

// emitProbe commits one probe event to the outbox through db, in its own
// transaction, under the service's CloudEvents source, and returns its id.
func emitProbe(t *testing.T, db *sqlate.DB) string {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("probe-%d", time.Now().UnixNano())
	rec := event.NewRecorder(ob.Sink(), "web-service", event.IDs(func() string { return id }))
	ctx := context.Background()
	_, err = sqlate.Transact(ctx, db, rec.Emit(ctx, func(_ *sqlate.Tx, q *event.Queue) (struct{}, error) {
		probe.Raise(q, "probe", probeData{Note: t.Name()})
		return struct{}{}, nil
	}))
	if err != nil {
		t.Fatalf("emit the probe event: %v", err)
	}
	return id
}

// pending counts the outbox rows awaiting publication.
func pending(t *testing.T, db *sqlate.DB) int {
	t.Helper()
	n, err := msgpg.Pending(context.Background(), db)
	if err != nil {
		t.Fatalf("count the pending outbox rows: %v", err)
	}
	return n
}

// drained waits until the outbox holds nothing pending: every event
// committed so far, a seed's among them, is published.
func drained(t *testing.T, db *sqlate.DB) {
	t.Helper()
	processtest.WaitFor(t, "the outbox drained", func() bool { return pending(t, db) == 0 })
}

// outboxHolds reports whether the outbox holds the row of the event with
// id, published or not.
func outboxHolds(t *testing.T, db *sqlate.DB, id string) bool {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT 1 FROM messaging_outbox WHERE id = $1", id)
	if err != nil {
		t.Fatalf("read the outbox row %s: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	held := rows.Next()
	if err := rows.Err(); err != nil {
		t.Fatalf("read the outbox row %s: %v", id, err)
	}
	return held
}

// eventRecords is out's records with msg msg naming the event id.
func eventRecords(out, msg, id string) []map[string]any {
	var matched []map[string]any
	for _, r := range withMsg(out, msg) {
		if r["event"] == id {
			matched = append(matched, r)
		}
	}
	return matched
}

// unreadyChecks is the names of the readiness report's checks that are not
// ready, in its order.
func unreadyChecks(t *testing.T, c *webtest.Client) (status int, unready []string) {
	t.Helper()
	r := c.Get(t, "/readyz")
	// The checks alone: a 503 is a problem, whose status is its number.
	var report struct {
		Checks []check `json:"checks"`
	}
	r.JSON(t, &report)
	if names := checkNames(report.Checks); !slices.Equal(names, readyChecks) {
		t.Errorf("readyz checks = %v, want %v", names, readyChecks)
	}
	for _, ch := range report.Checks {
		if !ch.Ready {
			unready = append(unready, ch.Name)
		}
	}
	return r.Status, unready
}

// The relay publishes a committed outbox event to the broker: the row the
// probe commits is published once, the service logging the event it sent,
// and then marked published, so the outbox holds nothing pending. The
// reset commits its own event and its seed's first, so the case waits for
// them to drain before it commits the probe, and the probe's row is then
// the only one the relay has to publish. The relay logs the publish before
// it marks the row in the same pass, so the case waits for the mark rather
// than reading the outbox the moment the log appears.
func TestMessaging_RelayPublishesCommittedEvents(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	assertReady(t, c)
	db := integration.Database(t)
	drained(t, db)

	id := emitProbe(t, db)
	processtest.WaitFor(t, "the probe event published", func() bool {
		return len(eventRecords(s.Output(), "event published", id)) == 1
	})
	processtest.WaitFor(t, "the probe's row marked published", func() bool { return pending(t, db) == 0 })
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
	if n := len(eventRecords(s.Output(), "event published", id)); n != 1 {
		t.Errorf("the service's log records the probe's publish %d times; want once", n)
	}
}

// A broker outage is a temporary condition, as a database outage is: while
// NATS is unreachable the process keeps running and the readiness probe
// answers 503 naming the broker check alone, and when NATS returns the
// broker reconnects and the service is ready again without a restart.
func TestMessaging_BrokerOutage(t *testing.T) {
	f := processtest.Forward(t, integration.NATSAddr())
	s := integration.Start(t, integration.Options{NATS: f.Addr()})
	c := s.Client()
	assertReady(t, c)

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})

	processtest.WaitFor(t, "the broker check unready", func() bool {
		status, unready := unreadyChecks(t, c)
		return status == http.StatusServiceUnavailable && slices.Equal(unready, []string{"broker"})
	})
	c.Get(t, "/healthz").Expect(t, http.StatusOK) // the process is up; only the dependency is down
	if s.Exited() {
		t.Fatalf("the service exited during the outage:\n%s", s.Output())
	}

	f.Restore(t)
	restored = true

	processtest.WaitFor(t, "readiness after NATS returns", func() bool {
		return c.Get(t, "/readyz").Status == http.StatusOK
	})
	assertReady(t, c)
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}

// A state reset with an event pending returns while the relay runs: the
// relay holds the quiesce gate for each row and the reset holds it
// exclusively, so the reset waits out the row in flight and the relay
// claims nothing while the messaging set is reverted and applied again.
// The event is held pending by severing NATS, so its publish fails and its
// row stays in the outbox until the reset reverts it. The reset commits its
// own web-service.database.reset, then its seed its creations' events,
// every seed event type, to the outbox applied again, and their publishes
// fail too while NATS is severed. From the reset's start the relay reports
// no failure but those refused publishes, a database error among them,
// and once NATS returns the service is ready, the relay check with it,
// with nothing pending.
func TestMessaging_StateResetUnderTheRelay(t *testing.T) {
	f := processtest.Forward(t, integration.NATSAddr())
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		NATS: f.Addr(),
		// A publish to the severed broker gives up within a second, so the
		// row in flight releases the gate promptly, and the relay polls
		// again within milliseconds, so an ungated relay would claim while
		// the reset runs rather than a poll after it.
		Env: []string{"APP_MESSAGING_RELAY_TIMEOUT=1s", "APP_MESSAGING_RELAY_POLL=5ms"},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	db := integration.Database(t)
	// The reset committed its event and its seed's; the probe's row is to
	// be the only one pending once NATS is severed.
	drained(t, db)

	f.Sever()
	restored := false
	t.Cleanup(func() {
		if !restored {
			f.Restore(t)
		}
	})
	id := emitProbe(t, db)
	processtest.WaitFor(t, "the probe's publish refused", func() bool {
		return len(eventRecords(s.Output(), "messaging failure", id)) > 0
	})
	if n := pending(t, db); n != 1 {
		t.Fatalf("pending = %d before the reset, want the probe's row", n)
	}

	// The window the gate must cover is the reset itself, while the
	// messaging set is reverted and applied again, so the failures read
	// below are every one from here on.
	before := len(s.Output())
	tr := integration.Reset(t, c, integration.Default)
	if m := tr.Schema.Set("messaging"); m.Version != m.Latest || m.Latest < 1 || !tr.Schema.Ready {
		t.Errorf("schema after the reset = %+v, want the messaging set current and the schema ready", tr.Schema)
	}
	if outboxHolds(t, db, id) {
		t.Errorf("the outbox holds the probe's row after the reset; want it reverted")
	}

	f.Restore(t)
	restored = true
	processtest.WaitFor(t, "readiness after the reset and NATS's return", func() bool {
		return c.Get(t, "/readyz").Status == http.StatusOK
	})
	assertReady(t, c)
	drained(t, db)
	// The reset's event, held pending while NATS was severed, is published
	// once NATS returns, about the state the reset applied.
	var resets []map[string]any
	for _, r := range withMsg(s.Output()[before:], "event published") {
		if r["type"] == stateReset {
			resets = append(resets, r)
		}
	}
	if len(resets) != 1 || resets[0]["subject"] != integration.Default {
		t.Errorf("published %s records = %v; want one about %s", stateReset, resets, integration.Default)
	}
	// The probe's row reports its refused publish until the reset reverts
	// it, the row in flight as the reset began among them, and the reset's
	// event or a seed's, of any type a seed commits, its refused publish
	// until NATS returns; any other failure, such as a claim against the
	// reverted outbox, is the relay's running beside the reset.
	resetTypes := append([]string{stateReset}, seedTypes...)
	for _, r := range withMsg(s.Output()[before:], "messaging failure") {
		committed := slices.Contains(resetTypes, fmt.Sprint(r["type"])) && strings.Contains(fmt.Sprint(r["error"]), "nats: publish")
		if r["event"] != id && !committed {
			t.Errorf("the relay reported a failure during or after the reset: %v", r)
		}
	}
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
}

// indexOf is the position in out of the first record with msg msg, or -1.
func indexOf(out, msg string, id string) int {
	for i, r := range logRecords(out) {
		if r["msg"] == msg && (id == "" || r["event"] == id) {
			return i
		}
	}
	return -1
}

// The drain runs the server before the relay: an event committed while the
// server drains, with a request still in flight, is published before the
// process exits. The request is an upload held open partway, so the server
// is still draining; the event is the suite's probe, committed beneath the
// API once the server has stopped accepting connections, while the server
// still drains. Once the request is released the process exits cleanly, and
// its log records the publish before "server stopped". The request-driven
// case, the held upload's own event, is
// TestEvents_AnUploadInFlightAtTheInterruptIsPublished.
func TestMessaging_EventCommittedDuringTheDrainIsPublished(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	db := integration.Database(t)
	// The reset committed its event and its seed's; the drain is to
	// publish the probe's.
	drained(t, db)
	held := hold(t, s, "/api/documents/"+tree(t, c)[docsOrg].ID+"/directories/root/files?name=held.txt")

	exit := make(chan int, 1)
	go func() { exit <- s.Stop(t) }()
	processtest.WaitFor(t, "the server's listener closed for the drain", func() bool {
		conn, err := net.DialTimeout("tcp", s.Addr(), time.Second)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	})

	id := emitProbe(t, db)
	processtest.WaitFor(t, "the probe event published during the drain", func() bool {
		return len(eventRecords(s.Output(), "event published", id)) == 1
	})
	held.release()

	if code := <-exit; code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
	published, stopped := indexOf(s.Output(), "event published", id), indexOf(s.Output(), "server stopped", "")
	if published < 0 || stopped < 0 || published > stopped {
		t.Errorf("event published at record %d, server stopped at %d; want the publish before the stop", published, stopped)
	}
	if n := pending(t, db); n != 0 {
		t.Errorf("pending = %d after exit, want 0", n)
	}
}
