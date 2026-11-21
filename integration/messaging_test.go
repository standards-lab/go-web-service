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

type probeData struct {
	Note string `json:"note"`
}

var probe = event.Define[probeData]("web-service.integration.probe")

func emitProbe(t *testing.T, db *sqlate.DB) string {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("probe-%d", time.Now().UnixNano())
	rec := event.NewRecorder(ob.Sink(), "web-service", event.Options{IDs: func() string { return id }})
	ctx := context.Background()
	_, err = sqlate.Transact(ctx, db, rec.Emit(ctx, func(_ *sqlate.Tx, q *event.Queue) (struct{}, error) {
		probe.Raise(q, "probe", probeData{Note: t.Name()})
		return struct{}{}, nil
	}), nil)
	if err != nil {
		t.Fatalf("emit the probe event: %v", err)
	}
	return id
}

func pending(t *testing.T, db *sqlate.DB) int {
	t.Helper()
	n, err := msgpg.Pending(context.Background(), db)
	if err != nil {
		t.Fatalf("count the pending outbox rows: %v", err)
	}
	return n
}

func drained(t *testing.T, db *sqlate.DB) {
	t.Helper()
	processtest.WaitFor(t, "the outbox drained", func() bool { return pending(t, db) == 0 })
}

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

func eventRecords(out, msg, id string) []map[string]any {
	var matched []map[string]any
	for _, r := range withMsg(out, msg) {
		if r["event"] == id {
			matched = append(matched, r)
		}
	}
	return matched
}

func unreadyChecks(t *testing.T, c *webtest.Client) (status int, unready []string) {
	t.Helper()
	r := c.Get(t, "/readyz")
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
	c.Get(t, "/healthz").Expect(t, http.StatusOK)
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

func TestMessaging_StateResetUnderTheRelay(t *testing.T) {
	f := processtest.Forward(t, integration.NATSAddr())
	s := integration.Start(t, integration.Options{
		Seed: integration.Default,
		NATS: f.Addr(),
		Env:  []string{"APP_MESSAGING_RELAY_TIMEOUT=1s", "APP_MESSAGING_RELAY_POLL=5ms"},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	db := integration.Database(t)
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
	var resets []map[string]any
	for _, r := range withMsg(s.Output()[before:], "event published") {
		if r["type"] == stateReset {
			resets = append(resets, r)
		}
	}
	if len(resets) != 1 || resets[0]["subject"] != integration.Default {
		t.Errorf("published %s records = %v; want one about %s", stateReset, resets, integration.Default)
	}
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

func indexOf(out, msg string, id string) int {
	for i, r := range logRecords(out) {
		if r["msg"] == msg && (id == "" || r["event"] == id) {
			return i
		}
	}
	return -1
}

func TestMessaging_EventCommittedDuringTheDrainIsPublished(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	db := integration.Database(t)
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
