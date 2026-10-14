package events_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/standards-lab/go-core/event"

	"github.com/standards-lab/go-web-service/tools/slab/events"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// uploaded is an event as the service publishes it: a file upload, its
// subject the file's id and its data the file's metadata.
var uploaded = event.Event{
	ID:              "0199e7a0-0000-7000-8000-000000000001",
	Source:          "web-service",
	Type:            "web-service.document.file.uploaded",
	Subject:         "0199e7a0-0000-7000-8000-0000000000f1",
	DataContentType: "application/json",
	Time:            time.Date(2026, 10, 14, 9, 30, 0, 123456789, time.UTC),
	Data:            []byte(`{"file_id":"0199e7a0-0000-7000-8000-0000000000f1","name":"q3.pdf","size":42}`),
}

// message encodes e as go-messaging's NATS broker publishes it: binary
// content mode, the headers as the message's, with the deduplication key
// the broker adds beside them.
func message(t *testing.T, e event.Event) (natsgo.Header, []byte) {
	t.Helper()
	h, body, err := event.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	header := natsgo.Header(h)
	header.Set("Nats-Msg-Id", "11:web-service"+e.ID)
	return header, body
}

// show decodes the message for e and renders the event under st.
func show(t *testing.T, st style.Style, e event.Event) string {
	t.Helper()
	got, err := events.Decode(message(t, e))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var b bytes.Buffer
	if err := events.Render(st, got)(&b); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

// An encoded event decodes back to itself, the broker's own header
// ignored, and renders as its type, a table of its id, subject, and time,
// its data as indented JSON, and a blank line.
func TestRender_PrintsAnEncodedEventsTypeIDSubjectTimeAndData(t *testing.T) {
	h, body := message(t, uploaded)
	got, err := events.Decode(h, body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != uploaded.ID || got.Type != uploaded.Type || got.Subject != uploaded.Subject || !got.Time.Equal(uploaded.Time) || !bytes.Equal(got.Data, uploaded.Data) || got.Extensions != nil {
		t.Errorf("Decode = %+v, want %+v", got, uploaded)
	}

	want := "web-service.document.file.uploaded\n" +
		"id       0199e7a0-0000-7000-8000-000000000001\n" +
		"subject  0199e7a0-0000-7000-8000-0000000000f1\n" +
		"time     2026-10-14T09:30:00.123456789Z\n" +
		"{\n" +
		"  \"file_id\": \"0199e7a0-0000-7000-8000-0000000000f1\",\n" +
		"  \"name\": \"q3.pdf\",\n" +
		"  \"size\": 42\n" +
		"}\n" +
		"\n"
	if out := show(t, style.New(false), uploaded); out != want {
		t.Errorf("Render wrote:\n%s\nwant:\n%s", out, want)
	}
}

// With color on, the type is styled as a status line and the table's
// names as keys, as a direct command's response is.
func TestRender_ColorsTheTypeAndTheNamesWhenOn(t *testing.T) {
	st := style.New(true)
	out := show(t, st, uploaded)
	for _, want := range []string{st.Status(uploaded.Type), st.Key("id"), st.Key("subject"), st.Key("time")} {
		if !strings.Contains(out, want) {
			t.Errorf("Render wrote %q, want it to contain %q", out, want)
		}
	}
}

// Absent attributes and data print nothing; text data prints as it came;
// binary data prints as its size and media type.
func TestRender_PrintsWhatTheEventCarries(t *testing.T) {
	bare := event.Event{ID: "e1", Source: "web-service", Type: "web-service.thing.happened"}
	text := bare
	text.Data, text.DataContentType = []byte("plain words"), "text/plain"
	binary := bare
	binary.Data, binary.DataContentType = []byte{0xff, 0xfe, 0x00}, "application/octet-stream"
	for name, tc := range map[string]struct {
		e    event.Event
		want string
	}{
		"no subject, time, or data": {bare, "web-service.thing.happened\nid  e1\n\n"},
		"text data":                 {text, "web-service.thing.happened\nid  e1\nplain words\n\n"},
		"binary data":               {binary, "web-service.thing.happened\nid  e1\n3 bytes of data (application/octet-stream)\n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if out := show(t, style.New(false), tc.e); out != tc.want {
				t.Errorf("Render wrote %q, want %q", out, tc.want)
			}
		})
	}
}

// A message that is not an event in binary content mode does not decode.
func TestDecode_RefusesAMessageThatIsNotAnEvent(t *testing.T) {
	h, body := message(t, uploaded)
	h.Del("ce-specversion")
	if _, err := events.Decode(h, body); err == nil || !strings.Contains(err.Error(), "specversion") {
		t.Errorf("Decode = %v, want the missing specversion's error", err)
	}
}

// NewBroker refuses what go-messaging's NATS configuration refuses, doing
// no I/O.
func TestNewBroker_RefusesAnInvalidConfiguration(t *testing.T) {
	if _, err := events.NewBroker("nats://127.0.0.1:4222", "WEB_SERVICE", "web-service.>"); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Errorf("NewBroker = %v, want the prefix's error", err)
	}
}

// A broker binds the stream, and its filter is every subject under the
// prefix, the stream's own subjects.
func TestNewBroker_FiltersOnThePrefix(t *testing.T) {
	b, err := events.NewBroker("nats://127.0.0.1:4222", "WEB_SERVICE", "web-service")
	if err != nil {
		t.Fatal(err)
	}
	if b.Stream() != "WEB_SERVICE" || b.Filter() != "web-service.>" {
		t.Errorf("stream, filter = %q, %q, want WEB_SERVICE, web-service.>", b.Stream(), b.Filter())
	}
}

// Start with nothing answering names the mise task that brings the stack
// up, and wraps the client's own error.
func TestStart_NamesTheTaskThatBringsTheStackUp(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "nats://" + l.Addr().String()
	_ = l.Close()
	b, err := events.NewBroker(url, "WEB_SERVICE", "web-service")
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start(context.Background())
	if want := "nothing answers at " + url + " (start the stack with: mise run db:up): "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Start = %v, want it to start with %q", err, want)
	}
	if !errors.Is(err, natsgo.ErrNoServers) {
		t.Errorf("Start = %v, want it to wrap %v", err, natsgo.ErrNoServers)
	}
}

// Start in a run that has already ended returns the context's error alone,
// with no hint, since no task would fix it.
func TestStart_ReturnsTheContextsErrorOnceTheRunHasEnded(t *testing.T) {
	b, err := events.NewBroker("nats://127.0.0.1:1", "WEB_SERVICE", "web-service")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Start(ctx); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "mise run") {
		t.Errorf("Start = %v, want context.Canceled alone", err)
	}
}
