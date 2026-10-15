package outboxtest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"
)

// Source, ID, and Time are what a Stamped recorder stamps on every event:
// the service's source, and an id and a time fixed so a test can assert
// them.
const (
	Source = "web-service"
	ID     = "00000000-0000-7000-8000-0000000000ee"
)

// Time is the time a Stamped recorder stamps on every event.
var Time = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// Stamped is the recorder the composition root injects, over sink, with
// the id and the time it stamps fixed to ID and Time.
func Stamped(sink event.Sink[*sqlate.Tx]) *event.Recorder[*sqlate.Tx] {
	return event.NewRecorder(sink, Source,
		event.IDs(func() string { return ID }), event.Clock(func() time.Time { return Time }))
}

// IsWrite reports whether c is the outbox's insert of an event.
func IsWrite(c sqltest.Call) bool {
	return c.Op == sqltest.OpExec && strings.Contains(c.SQL, "messaging_outbox")
}

// Writes is every outbox insert rec recorded, in order, committed or not.
func Writes(rec *sqltest.Recorder) []sqltest.Call {
	var out []sqltest.Call
	for _, c := range rec.Calls() {
		if IsWrite(c) {
			out = append(out, c)
		}
	}
	return out
}

// Decode is the event the outbox insert c wrote: its parameters are the
// source, the id, the header as JSON, and the data, in that order.
func Decode(t testing.TB, c sqltest.Call) event.Event {
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

// bytesOf is a bound argument's bytes, as the driver was handed them.
func bytesOf(t testing.TB, arg any) []byte {
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

// Committed is every event a committed transaction wrote, in commit order:
// an outbox insert counts once its transaction commits, and one a rollback
// undid never does.
func Committed(t testing.TB, rec *sqltest.Recorder) []event.Event {
	t.Helper()
	var out, open []event.Event
	for _, c := range rec.Calls() {
		switch {
		case IsWrite(c):
			open = append(open, Decode(t, c))
		case c.Op == sqltest.OpRollback:
			open = nil
		case c.Op == sqltest.OpCommit:
			out, open = append(out, open...), nil
		}
	}
	return out
}

// NoneCommitted fails the test if any event committed: a write its
// transaction rolled back is no event.
func NoneCommitted(t testing.TB, rec *sqltest.Recorder) {
	t.Helper()
	if events := Committed(t, rec); len(events) != 0 {
		t.Errorf("events committed = %d; want none (ops %v)", len(events), rec.Ops())
	}
}

// Want fails the test unless e is typ about subject, stamped by a Stamped
// recorder, with JSON data exactly want: every field, and no other.
func Want(t testing.TB, e event.Event, typ, subject string, want map[string]any) {
	t.Helper()
	WantEnvelope(t, e, typ, subject)
	var got map[string]any
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatalf("decode the data %s: %v", e.Data, err)
	}
	if len(got) != len(want) {
		t.Errorf("data = %v; want exactly %v", got, want)
	}
	for k, v := range want {
		if w, ok := got[k]; !ok || w != v {
			t.Errorf("data[%q] = %v; want %v", k, got[k], v)
		}
	}
}

// WantEnvelope fails the test unless e is typ about subject, stamped by a
// Stamped recorder, with JSON data, whatever its shape: Want's check of
// the envelope, for data that is not an object.
func WantEnvelope(t testing.TB, e event.Event, typ, subject string) {
	t.Helper()
	if e.Type != typ || e.Subject != subject || e.Source != Source || e.ID != ID ||
		!e.Time.Equal(Time) || e.DataContentType != "application/json" {
		t.Errorf("event = %+v; want %s about %s, stamped by the recorder", e, typ, subject)
	}
}
