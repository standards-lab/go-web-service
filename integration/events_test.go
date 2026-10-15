//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/domain/document"
	orgdomain "github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/integration"
)

// uploaded is the document upload's event type, and the marker's.
var uploaded = document.FileUploaded.Type()

// The logo commands' event types.
var (
	logoUploaded = orgdomain.LogoUploaded.Type()
	logoDeleted  = orgdomain.LogoDeleted.Type()
)

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

// Seeding emits no upload: neither the seed a startup applies to an empty
// schema, which writes every seeded file and logo, nor a state reset's.
// (A seeded document root emits its directory.created, as every root's
// creation does; the case does not subscribe to it.) The subscriber binds while a first process runs, which reverts the
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

// A logo's upload is no document upload: it emits exactly one
// web-service.organization.logo.uploaded, about the logo's file, and no
// web-service.organization.logo.deleted for the seeded logo it replaces.
func TestEvents_ALogoEmitsLogoUploaded(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, uploaded, logoUploaded, logoDeleted)
	all := tree(t, c)
	logo := webtest.Decode[identity](t, c.Put(t, organizations+"/"+all["logistics"].ID+"/logo", webtest.Raw{ContentType: "image/png", Body: []byte("png")}), http.StatusCreated)
	m, got := marker(t, c, events, "/api/documents/"+all[docsOrg].ID, "after-logo.txt")
	if l := about(got, logo.ID); len(l) != 1 || l[0].Type != logoUploaded {
		t.Errorf("events about the logo = %v; want one %s", subjects(l), logoUploaded)
	}
	if n := len(about(got, m.ID)); n != 1 || len(got) != 2 {
		t.Errorf("events = %v; want the logo's and the marker's alone", subjects(got))
	}
}

// commandTypes is every type a command case's event may carry, the types
// the commands' case subscribes to beside the marker's.
var commandTypes = []string{
	orgdomain.OrganizationCreated.Type(), orgdomain.OrganizationEdited.Type(),
	orgdomain.OrganizationTransferred.Type(), orgdomain.OrganizationDeleted.Type(),
	logoUploaded, logoDeleted,
	document.DirectoryCreated.Type(), document.DirectoryMoved.Type(), document.DirectoryDeleted.Type(),
}

// commandFixture is what a command case runs against: the client, the
// default state's organizations by code, as the case found them, and the
// document API of the documents' organization.
type commandFixture struct {
	c    *webtest.Client
	orgs map[string]organization
	docs string
}

// mkdir creates a directory named name under parent, the root alias or a
// directory id, in the fixture's documents.
func (f commandFixture) mkdir(t *testing.T, parent, name string) identity {
	t.Helper()
	return webtest.Decode[identity](t, f.c.Post(t, f.docs+"/directories", map[string]string{"parent_id": parent, "name": name}), http.StatusCreated)
}

// commandCase is one command and the event it commits: run performs it,
// after any setup of its own, and returns the event's type and the id of
// the entity the type names, the event's subject. Each case leaves the
// organizations the others use as it found them, or uses its own.
type commandCase struct {
	name string
	run  func(t *testing.T, f commandFixture) (typ, subject string)
}

// commandCases is every command the service emits an event for, one row
// each.
var commandCases = []commandCase{
	{"organization create", func(t *testing.T, f commandFixture) (string, string) {
		id := webtest.Decode[identity](t, f.c.Post(t, organizations, map[string]any{"parent_id": f.orgs["acme"].ID, "code": "events-created", "name": "Created"}), http.StatusCreated)
		return orgdomain.OrganizationCreated.Type(), id.ID
	}},
	{"organization edit", func(t *testing.T, f commandFixture) (string, string) {
		o := f.orgs["product"]
		webtest.Decode[identity](t, f.c.Put(t, organizations+"/"+o.ID, map[string]any{"code": o.Code, "name": "Product Line"}, webtest.IfMatch(o.Version)), http.StatusOK)
		return orgdomain.OrganizationEdited.Type(), o.ID
	}},
	{"organization transfer", func(t *testing.T, f commandFixture) (string, string) {
		o := f.orgs["platform"]
		webtest.Decode[identity](t, f.c.Post(t, organizations+"/"+o.ID+"/transfer", map[string]any{"parent_id": f.orgs["operations"].ID}, webtest.IfMatch(o.Version)), http.StatusOK)
		return orgdomain.OrganizationTransferred.Type(), o.ID
	}},
	{"organization delete", func(t *testing.T, f commandFixture) (string, string) {
		id := webtest.Decode[identity](t, f.c.Post(t, organizations, map[string]any{"parent_id": f.orgs["acme"].ID, "code": "events-deleted", "name": "Deleted"}), http.StatusCreated)
		f.c.Delete(t, organizations+"/"+id.ID, webtest.IfMatch(id.Version)).Expect(t, http.StatusNoContent)
		return orgdomain.OrganizationDeleted.Type(), id.ID
	}},
	{"logo put", func(t *testing.T, f commandFixture) (string, string) {
		id := webtest.Decode[identity](t, f.c.Put(t, organizations+"/"+f.orgs["logistics"].ID+"/logo", webtest.Raw{ContentType: "image/png", Body: []byte("png")}), http.StatusCreated)
		return logoUploaded, id.ID
	}},
	{"logo delete", func(t *testing.T, f commandFixture) (string, string) {
		logo := organizations + "/" + f.orgs["engineering"].ID + "/logo"
		id := webtest.Decode[identity](t, f.c.Put(t, logo, webtest.Raw{ContentType: "image/png", Body: []byte("png")}), http.StatusCreated)
		f.c.Delete(t, logo).Expect(t, http.StatusNoContent)
		return logoDeleted, id.ID
	}},
	{"directory create", func(t *testing.T, f commandFixture) (string, string) {
		return document.DirectoryCreated.Type(), f.mkdir(t, "root", "events-created").ID
	}},
	{"directory delete", func(t *testing.T, f commandFixture) (string, string) {
		id := f.mkdir(t, "root", "events-deleted")
		f.c.Delete(t, f.docs+"/directories/"+id.ID, webtest.IfMatch(id.Version)).Expect(t, http.StatusNoContent)
		return document.DirectoryDeleted.Type(), id.ID
	}},
	{"directory delete branch", func(t *testing.T, f commandFixture) (string, string) {
		id := f.mkdir(t, "root", "events-branch")
		f.mkdir(t, id.ID, "beneath")
		f.c.Delete(t, f.docs+"/directories/"+id.ID+"?recursive=true", webtest.IfMatch(id.Version)).Expect(t, http.StatusAccepted)
		return document.DirectoryDeleted.Type(), id.ID
	}},
	{"directory move", func(t *testing.T, f commandFixture) (string, string) {
		to := f.mkdir(t, "root", "events-destination")
		id := f.mkdir(t, "root", "events-moved")
		webtest.Decode[identity](t, f.c.Post(t, f.docs+"/directories/"+id.ID+"/move", map[string]string{"parent_id": to.ID, "name": "events-moved"}, webtest.IfMatch(id.Version)), http.StatusOK)
		return document.DirectoryMoved.Type(), id.ID
	}},
}

// Each command commits exactly one event of its type about the entity the
// type names, published on its type from the service's source, which a
// subscriber that does not own the stream receives. Each case is followed
// by a marker upload, so every event its command committed has arrived by
// the marker's.
func TestEvents_EachCommandEmitsItsEvent(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	events := integration.Subscribe(t, append([]string{uploaded}, commandTypes...)...)
	orgs := tree(t, c)
	f := commandFixture{c: c, orgs: orgs, docs: "/api/documents/" + orgs[docsOrg].ID}
	for i, cc := range commandCases {
		t.Run(cc.name, func(t *testing.T) {
			typ, subject := cc.run(t, f)
			_, got := marker(t, c, events, f.docs, fmt.Sprintf("after-command-%d.txt", i))
			var match []event.Event
			for _, e := range about(got, subject) {
				if e.Type == typ {
					match = append(match, e)
				}
			}
			if len(match) != 1 {
				t.Fatalf("%s events about %s = %d (%v); want one", typ, subject, len(match), subjects(got))
			}
			if e := match[0]; e.Source != "web-service" || e.ID == "" || e.Time.IsZero() || e.DataContentType != "application/json" {
				t.Errorf("event = %+v; want it from web-service, stamped", e)
			}
		})
	}
}

// An organization's first write under the root alias creates its document
// root, and the root's web-service.document.directory.created is delivered
// before the write's own: the root's commits in the transaction that
// creates it, the write's in its own after it. The root's data shows it
// as the API does, with no parent and named "/". A second write, which
// finds the root, commits no root event.
func TestEvents_AFirstWriteUnderTheRootEmitsTheRootsCreateFirst(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	created := document.DirectoryCreated.Type()
	events := integration.Subscribe(t, created)
	org := webtest.Decode[identity](t, c.Post(t, organizations, map[string]any{"parent_id": tree(t, c)["acme"].ID, "code": "events-rootless", "name": "Rootless"}), http.StatusCreated)
	docs := "/api/documents/" + org.ID

	first := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "first"}), http.StatusCreated)
	second := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "second"}), http.StatusCreated)
	root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)

	// The reset's seed may still be relaying the seeded roots' events, so
	// the case reads only those about its own directories.
	var got []event.Event
	for _, e := range events.Await(t, second.ID) {
		if e.Subject == root.ID || e.Subject == first.ID || e.Subject == second.ID {
			got = append(got, e)
		}
	}
	if want := []string{created + " " + root.ID, created + " " + first.ID, created + " " + second.ID}; fmt.Sprint(subjects(got)) != fmt.Sprint(want) {
		t.Fatalf("events = %v; want the root's, then the first write's, then the second's", subjects(got))
	}
	var data map[string]any
	if err := json.Unmarshal(got[0].Data, &data); err != nil {
		t.Fatalf("decode the root's data %s: %v", got[0].Data, err)
	}
	want := map[string]any{"directory_id": root.ID, "organization_id": org.ID, "parent_id": nil, "name": "/", "version": float64(root.Version)}
	if len(data) != len(want) {
		t.Errorf("data = %v; want exactly %v", data, want)
	}
	for k, v := range want {
		if w, ok := data[k]; !ok || w != v {
			t.Errorf("data[%q] = %v; want %v", k, data[k], v)
		}
	}
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
