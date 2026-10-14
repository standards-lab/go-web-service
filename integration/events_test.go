//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/integration"
)

// uploaded is the document upload's event type.
var uploaded = document.FileUploaded.Type()

// fileMeta is the file's metadata as the API presents it: the fields its
// upload's event carries.
type fileMeta struct {
	ID          string `json:"id"`
	DirectoryID string `json:"directory_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Size        *int64 `json:"size"`
	ContentType string `json:"content_type"`
	Version     int64  `json:"version"`
}

// about is the events among got whose subject is subject.
func about(got []event.Event, subject string) []event.Event {
	var out []event.Event
	for _, e := range got {
		if e.Subject == subject {
			out = append(out, e)
		}
	}
	return out
}

// marker uploads a file named name into the root of docs and waits for its
// event, returning the identity and every event received by then. The
// relay publishes in commit order, so an event any earlier command
// committed has arrived by the marker's.
func marker(t *testing.T, c *webtest.Client, events *integration.Stream, docs, name string) (identity, []event.Event) {
	t.Helper()
	id := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
	return id, events.Await(t, id.ID)
}

// onlyMarkers fails the test unless every event in got is about one of
// the markers, each exactly once: whatever the case ran before them
// emitted nothing.
func onlyMarkers(t *testing.T, got []event.Event, markers ...string) {
	t.Helper()
	for _, m := range markers {
		if n := len(about(got, m)); n != 1 {
			t.Errorf("events about the marker %s = %d; want one", m, n)
		}
	}
	if len(got) != len(markers) {
		t.Errorf("events = %d (%v); want the %d markers' alone", len(got), subjects(got), len(markers))
	}
}

func subjects(es []event.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Type + " " + e.Subject
	}
	return out
}

// An upload emits exactly one web-service.document.file.uploaded, about
// the file, from the service's source, its data the file's metadata as the
// API presents it, the entity tag its download answers with, and the
// organization; never its content.
func TestEvents_AnUploadEmitsFileUploaded(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, uploaded)
	org := tree(t, c)[docsOrg].ID
	docs := "/api/documents/" + org

	body := []byte("quarterly report")
	id := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: body}), http.StatusCreated)
	meta := webtest.Decode[fileMeta](t, c.Get(t, docs+"/files/"+id.ID), http.StatusOK)
	etag := c.Get(t, docs+"/files/"+id.ID+"/content").Expect(t, http.StatusOK).Header.Get("ETag")

	got := about(events.Await(t, id.ID), id.ID)
	if len(got) != 1 {
		t.Fatalf("events about the upload = %d; want one", len(got))
	}
	e := got[0]
	if e.Type != uploaded || e.Source != "web-service" || e.ID == "" || e.Time.IsZero() || e.DataContentType != "application/json" {
		t.Errorf("event = %+v; want %s from web-service, stamped", e, uploaded)
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("decode the event's data %s: %v", e.Data, err)
	}
	want := map[string]any{
		"file_id": id.ID, "organization_id": org, "directory_id": meta.DirectoryID, "name": "q3.txt",
		"content_type": meta.ContentType, "size": float64(*meta.Size), "etag": etag, "version": float64(id.Version),
	}
	if meta.Status != "available" || meta.Version != id.Version || *meta.Size != int64(len(body)) || etag == "" {
		t.Fatalf("the uploaded file reads %+v with ETag %q; want it available at the upload's version", meta, etag)
	}
	if len(data) != len(want) {
		t.Errorf("data = %v; want exactly %v", data, want)
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q] = %v; want %v", k, data[k], v)
		}
	}
	if bytes.Contains(e.Data, body) {
		t.Errorf("the event's data %s carries the file's content", e.Data)
	}
}

// An upload that fails emits nothing, however it fails: a put the severed
// object store refuses, a completion the severed database refuses after
// the put, and a completion refused because the delete of the upload's
// directory reached the pending row first, its object deleted. Each case
// is followed by a marker upload, whose event arrives alone.
func TestEvents_AFailedUploadEmitsNothing(t *testing.T) {
	store := processtest.Forward(t, integration.StorageAddr())
	db := processtest.Forward(t, integration.DatabaseAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: store.Addr(), Database: db.Addr()})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, uploaded)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	var markers []string
	sever := func(t *testing.T, f *processtest.Forwarder) (restore func()) {
		f.Sever()
		restored := false
		t.Cleanup(func() {
			if !restored {
				f.Restore(t)
			}
		})
		return func() {
			f.Restore(t)
			restored = true
			processtest.WaitFor(t, "readiness after the dependency returns", func() bool {
				return c.Get(t, "/readyz").Status == http.StatusOK
			})
		}
	}
	quiet := func(t *testing.T, name string) {
		t.Helper()
		m, got := marker(t, c, events, docs, name)
		markers = append(markers, m.ID)
		onlyMarkers(t, got, markers...)
	}

	t.Run("a put the store refuses", func(t *testing.T) {
		restore := sever(t, store)
		_ = slowPost(t, s, docs+"/directories/root/files?name=refused.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("refused")}).Problem(t, http.StatusServiceUnavailable)
		restore()
		quiet(t, "after-put.txt")
	})

	t.Run("a completion the database refuses", func(t *testing.T) {
		held := hold(t, s, docs+"/directories/root/files?name=uncompleted.txt")
		restore := sever(t, db)
		if status, body := held.finish(t); status != http.StatusServiceUnavailable {
			t.Errorf("the upload completed over a severed database answered %d %s; want 503", status, body)
		}
		restore()
		quiet(t, "after-completion.txt")
	})

	t.Run("a completion the directory's delete refused", func(t *testing.T) {
		dir := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "inbox"}), http.StatusCreated)
		held := hold(t, s, docs+"/directories/"+dir.ID+"/files?name=swept.txt")
		markBranch(t, c, docs, dir.ID)
		if status, body := held.finish(t); status != http.StatusConflict && status != http.StatusNotFound {
			t.Errorf("the upload completed into a deleted directory answered %d %s; want 409 or 404", status, body)
		}
		quiet(t, "after-delete.txt")
	})
}

// Seeding emits nothing: neither the seed a startup applies to an empty
// schema, which writes every seeded file and logo, nor a state reset's.
// The subscriber binds while a first process runs, which reverts the
// schema and stops; a second starts over the empty schema and seeds it.
func TestEvents_SeedsEmitNothing(t *testing.T) {
	first := integration.Start(t, integration.Options{Seed: integration.Default})
	events := integration.Subscribe(t, uploaded)
	integration.Revert(t, first.Client())
	if code := first.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, first.Output())
	}

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	started, got := marker(t, c, events, docs, "after-startup.txt")
	onlyMarkers(t, got, started.ID)

	if tr := integration.Reset(t, c, integration.Default); tr.Seeded["documents"] == 0 || tr.Seeded["logos"] == 0 {
		t.Fatalf("the reset seeded %v; want documents and logos written", tr.Seeded)
	}
	// The reset seeds the organizations again under new ids.
	reset, got := marker(t, c, events, "/api/documents/"+tree(t, c)[docsOrg].ID, "after-reset.txt")
	onlyMarkers(t, got, started.ID, reset.ID)
}

// A logo's upload is no document upload, and emits nothing.
func TestEvents_ALogoEmitsNothing(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, uploaded)
	all := tree(t, c)
	webtest.Decode[identity](t, c.Put(t, organizations+"/"+all["logistics"].ID+"/logo", webtest.Raw{ContentType: "image/png", Body: []byte("png")}), http.StatusCreated)
	m, got := marker(t, c, events, "/api/documents/"+all[docsOrg].ID, "after-logo.txt")
	onlyMarkers(t, got, m.ID)
}

// The drain runs the server before the relay, so a request-driven
// producer needs no edge to it: an upload in flight when the interrupt
// arrives completes, commits its event in its completion's transaction,
// and the relay, still running while the server drains and its last pass
// at the latest, publishes the event before the process exits cleanly,
// leaving nothing pending.
func TestEvents_AnUploadInFlightAtTheInterruptIsPublished(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, uploaded)
	db := integration.Database(t)
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

	status, body := held.finish(t)
	if status != http.StatusCreated {
		t.Fatalf("the held upload answered %d %s; want 201", status, body)
	}
	var id identity
	if err := json.Unmarshal(body, &id); err != nil {
		t.Fatalf("decode the upload's identity %s: %v", body, err)
	}
	if code := <-exit; code != 0 {
		t.Fatalf("exit = %d:\n%s", code, s.Output())
	}
	got := about(events.Await(t, id.ID), id.ID)
	if len(got) != 1 {
		t.Fatalf("events about the held upload = %d; want one", len(got))
	}
	if n := len(eventRecords(s.Output(), "event published", got[0].ID)); n != 1 {
		t.Errorf("the service's log records the event's publish %d times before its exit; want once", n)
	}
	if n := pending(t, db); n != 0 {
		t.Errorf("pending = %d after exit, want 0", n)
	}
}
