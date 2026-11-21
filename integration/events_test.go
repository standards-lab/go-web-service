//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"

	dbadmin "github.com/standards-lab/go-web-service/admin/database"
	"github.com/standards-lab/go-web-service/domain/document"
	orgdomain "github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/integration"
)

var uploaded = document.FileUploaded.Type()

var (
	logoUploaded = orgdomain.LogoUploaded.Type()
	logoDeleted  = orgdomain.LogoDeleted.Type()
)

type fileMeta struct {
	ID          string `json:"id"`
	DirectoryID string `json:"directory_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Size        *int64 `json:"size"`
	ContentType string `json:"content_type"`
	Version     int64  `json:"version"`
}

func about(got []event.Event, subject string) []event.Event {
	var out []event.Event
	for _, e := range got {
		if e.Subject == subject {
			out = append(out, e)
		}
	}
	return out
}

func resetDrained(t *testing.T, c *webtest.Client) integration.Transition {
	t.Helper()
	tr := integration.Reset(t, c, integration.Default)
	drained(t, integration.Database(t))
	return tr
}

func marker(t *testing.T, c *webtest.Client, events *integration.Stream, docs, name string) (identity, []event.Event) {
	t.Helper()
	id := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
	return id, events.Await(t, id.ID)
}

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

func TestEvents_AnUploadEmitsFileUploaded(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
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

func TestEvents_AFailedUploadEmitsNothing(t *testing.T) {
	store := processtest.Forward(t, integration.StorageAddr())
	db := processtest.Forward(t, integration.DatabaseAddr())
	s := integration.Start(t, integration.Options{Seed: integration.Default, Storage: store.Addr(), Database: db.Addr()})
	c := s.Client()
	resetDrained(t, c)
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

var seedTypes = []string{
	orgdomain.OrganizationCreated.Type(), document.DirectoryCreated.Type(), uploaded, logoUploaded,
}

type declaredSeed struct {
	organizations []string
	logos         []string
	directories   []string
	files         []string
}

type seedEntry struct {
	ID      string      `json:"id"`
	Entries []seedEntry `json:"entries"`
	Content *string     `json:"content"`
}

func defaultSeed(t *testing.T) declaredSeed {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "data", "seeds", "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Organizations []struct {
			Code string `json:"code"`
		} `json:"organizations"`
		Logos []struct {
			ID string `json:"id"`
		} `json:"logos"`
		Documents []struct {
			Root    string      `json:"root"`
			Entries []seedEntry `json:"entries"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("decode the default state: %v", err)
	}
	var d declaredSeed
	for _, o := range st.Organizations {
		d.organizations = append(d.organizations, o.Code)
	}
	for _, l := range st.Logos {
		d.logos = append(d.logos, l.ID)
	}
	var walk func([]seedEntry)
	walk = func(es []seedEntry) {
		for _, e := range es {
			if e.Content != nil {
				d.files = append(d.files, e.ID)
				continue
			}
			d.directories = append(d.directories, e.ID)
			walk(e.Entries)
		}
	}
	for _, tr := range st.Documents {
		d.directories = append(d.directories, tr.Root)
		walk(tr.Entries)
	}
	return d
}

func bySubject(got []event.Event) map[string][]string {
	out := map[string][]string{}
	for _, e := range got {
		out[e.Type] = append(out[e.Type], e.Subject)
	}
	for _, subjects := range out {
		slices.Sort(subjects)
	}
	return out
}

func TestEvents_TheStartupSeedEmitsItsEvents(t *testing.T) {
	first := integration.Start(t, integration.Options{Seed: integration.Default})
	events := integration.Subscribe(t, seedTypes...)
	integration.Revert(t, first.Client())
	if code := first.Stop(t); code != 0 {
		t.Fatalf("exit = %d:\n%s", code, first.Output())
	}

	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	orgs := tree(t, c)
	m, got := marker(t, c, events, "/api/documents/"+orgs["acme"].ID, "after-startup.txt")
	got = slices.DeleteFunc(got, func(e event.Event) bool { return e.Subject == m.ID })

	theDefaultSeeds(t, got, orgs)
}

func theDefaultSeeds(t *testing.T, got []event.Event, orgs map[string]organization) {
	t.Helper()
	declared := defaultSeed(t)
	var orgIDs []string
	for _, code := range declared.organizations {
		orgIDs = append(orgIDs, orgs[code].ID)
	}
	want := map[string][]string{
		orgdomain.OrganizationCreated.Type(): orgIDs,
		document.DirectoryCreated.Type():     declared.directories,
		uploaded:                             declared.files,
		logoUploaded:                         declared.logos,
	}
	have := bySubject(got)
	for typ, subjects := range want {
		slices.Sort(subjects)
		if !slices.Equal(have[typ], subjects) {
			t.Errorf("%s events = %d about %v; want %d, one about each of %v", typ, len(have[typ]), have[typ], len(subjects), subjects)
		}
	}
	if len(have) != len(want) {
		t.Errorf("event types = %v; want the seed's alone", slices.Sorted(maps.Keys(have)))
	}
}

func TestEvents_ASeedOverASeededSchemaDeliversNothing(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
	events := integration.Subscribe(t, seedTypes...)
	if n := integration.Seed(t, c, integration.Default); !maps.Equal(n, integration.Seeded{"organizations": 0, "logos": 0, "documents": 0}) {
		t.Fatalf("seed over default = %v; want every contribution at zero", n)
	}
	m, got := marker(t, c, events, "/api/documents/"+tree(t, c)["acme"].ID, "after-rerun.txt")
	onlyMarkers(t, got, m.ID)
}

func TestEvents_ALogoEmitsLogoUploaded(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
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

var commandTypes = []string{
	orgdomain.OrganizationCreated.Type(), orgdomain.OrganizationEdited.Type(),
	orgdomain.OrganizationTransferred.Type(), orgdomain.OrganizationDeleted.Type(),
	logoUploaded, logoDeleted,
	document.DirectoryCreated.Type(), document.DirectoryMoved.Type(), document.DirectoryDeleted.Type(),
	document.FileMoved.Type(), document.FileDeleted.Type(),
}

type commandFixture struct {
	c    *webtest.Client
	orgs map[string]organization
	docs string
}

func (f commandFixture) mkdir(t *testing.T, parent, name string) identity {
	t.Helper()
	return webtest.Decode[identity](t, f.c.Post(t, f.docs+"/directories", map[string]string{"parent_id": parent, "name": name}), http.StatusCreated)
}

func (f commandFixture) upload(t *testing.T, parent, name string) identity {
	t.Helper()
	return webtest.Decode[identity](t, f.c.Post(t, f.docs+"/directories/"+parent+"/files?name="+name, webtest.Raw{ContentType: "text/plain", Body: []byte(name)}), http.StatusCreated)
}

type commandCase struct {
	name string
	run  func(t *testing.T, f commandFixture) (typ, subject string)
}

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
	{"file move", func(t *testing.T, f commandFixture) (string, string) {
		to := f.mkdir(t, "root", "events-file-destination")
		id := f.upload(t, "root", "events-file-moved.txt")
		webtest.Decode[identity](t, f.c.Post(t, f.docs+"/files/"+id.ID+"/move", map[string]string{"directory_id": to.ID, "name": "events-file-moved.txt"}, webtest.IfMatch(id.Version)), http.StatusOK)
		return document.FileMoved.Type(), id.ID
	}},
	{"file delete", func(t *testing.T, f commandFixture) (string, string) {
		id := f.upload(t, "root", "events-file-deleted.txt")
		f.c.Delete(t, f.docs+"/files/"+id.ID, webtest.IfMatch(id.Version)).Expect(t, http.StatusNoContent)
		return document.FileDeleted.Type(), id.ID
	}},
}

func TestEvents_EachCommandEmitsItsEvent(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
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

func TestEvents_ARepeatedFileDeleteDeliversNothingMore(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
	deleted := document.FileDeleted.Type()
	events := integration.Subscribe(t, uploaded, deleted)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID
	id := webtest.Decode[identity](t, c.Post(t, docs+"/directories/root/files?name=deleted-twice.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("twice")}), http.StatusCreated)

	deletes := func(t *testing.T, got []event.Event) int {
		t.Helper()
		n := 0
		for _, e := range about(got, id.ID) {
			if e.Type == deleted {
				n++
			}
		}
		return n
	}
	c.Delete(t, docs+"/files/"+id.ID, webtest.IfMatch(id.Version)).Expect(t, http.StatusNoContent)
	if _, got := marker(t, c, events, docs, "after-first-delete.txt"); deletes(t, got) != 1 {
		t.Fatalf("%s events about the file after its delete = %d (%v); want one", deleted, deletes(t, got), subjects(got))
	}
	_ = c.Delete(t, docs+"/files/"+id.ID, webtest.IfMatch(id.Version)).Problem(t, http.StatusNotFound)
	if _, got := marker(t, c, events, docs, "after-second-delete.txt"); deletes(t, got) != 1 {
		t.Errorf("%s events about the file after its repeated delete = %d (%v); want the first delete's alone", deleted, deletes(t, got), subjects(got))
	}
}

func TestEvents_AFirstWriteUnderTheRootEmitsTheRootsCreateFirst(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
	created := document.DirectoryCreated.Type()
	events := integration.Subscribe(t, created)
	org := webtest.Decode[identity](t, c.Post(t, organizations, map[string]any{"parent_id": tree(t, c)["acme"].ID, "code": "events-rootless", "name": "Rootless"}), http.StatusCreated)
	docs := "/api/documents/" + org.ID

	first := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "first"}), http.StatusCreated)
	second := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "second"}), http.StatusCreated)
	root := webtest.Decode[directory](t, c.Get(t, docs+"/directories/root"), http.StatusOK)

	got := events.Await(t, second.ID)
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

func TestEvents_AnUploadInFlightAtTheInterruptIsPublished(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
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

var stateReset = dbadmin.StateReset.Type()

func everyType() []string {
	return slices.Concat([]string{stateReset, probe.Type()}, seedTypes, commandTypes)
}

func afterProbe(t *testing.T, events *integration.Stream, skip int) []event.Event {
	t.Helper()
	id := emitProbe(t, integration.Database(t))
	var got []event.Event
	processtest.WaitFor(t, "the probe "+id, func() bool {
		got = events.Received()
		return slices.ContainsFunc(got, func(e event.Event) bool { return e.ID == id })
	})
	got = got[skip:]
	return got[:slices.IndexFunc(got, func(e event.Event) bool { return e.ID == id })]
}

func TestEvents_AStateResetDeliversItsEventBeforeItsSeed(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
	events := integration.Subscribe(t, everyType()...)

	for _, state := range []string{"empty", integration.Default} {
		skip := len(events.Received())
		integration.Reset(t, c, state)
		got := afterProbe(t, events, skip)
		if len(got) == 0 {
			t.Fatalf("reset to %s delivered nothing; want its event", state)
		}
		e := got[0]
		if e.Type != stateReset || e.Subject != state || e.Source != "web-service" {
			t.Errorf("reset to %s: first event = %s about %q from %q; want %s about the state from web-service", state, e.Type, e.Subject, e.Source, stateReset)
		}
		var data any
		if err := json.Unmarshal(e.Data, &data); err != nil {
			t.Fatalf("decode the reset's data %s: %v", e.Data, err)
		}
		if data != state {
			t.Errorf("reset to %s: data = %s; want the state's name, a JSON string", state, e.Data)
		}
		seeds := got[1:]
		if n := len(slices.DeleteFunc(slices.Clone(seeds), func(e event.Event) bool { return e.Type != stateReset })); n != 0 {
			t.Errorf("reset to %s: %d more %s events; want one", state, n, stateReset)
		}
		if state == "empty" {
			if len(seeds) != 0 {
				t.Errorf("reset to empty: events after its own = %v; want none", subjects(seeds))
			}
			continue
		}
		theDefaultSeeds(t, seeds, tree(t, c))
	}
}

func TestEvents_TheSchemaVerbsDeliverNothing(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	resetDrained(t, c)
	events := integration.Subscribe(t, everyType()...)
	head := integration.Schema(t, c).Set(integration.AppSet).Version

	for _, v := range []struct {
		path string
		body any
	}{
		{"/admin/database/schema/down", map[string]any{"set": integration.AppSet}},
		{"/admin/database/schema/up", nil},
		{"/admin/database/schema/steps", map[string]any{"set": integration.AppSet, "steps": -1}},
		{"/admin/database/schema/steps", map[string]any{"set": integration.AppSet, "steps": 1}},
		{"/admin/database/schema/force", map[string]any{"set": integration.AppSet, "version": head}},
	} {
		webtest.Decode[integration.SchemaStatus](t, c.Post(t, v.path, v.body), http.StatusOK)
	}
	if got := afterProbe(t, events, 0); len(got) != 0 {
		t.Errorf("the schema verbs delivered %v; want nothing", subjects(got))
	}
}

func TestEvents_TheSeedVerbDeliversItsCreationsAlone(t *testing.T) {
	s := integration.Start(t, integration.Options{Seed: integration.Default})
	c := s.Client()
	integration.Reset(t, c, "empty")
	drained(t, integration.Database(t))
	events := integration.Subscribe(t, everyType()...)

	integration.Seed(t, c, integration.Default)
	theDefaultSeeds(t, afterProbe(t, events, 0), tree(t, c))
}
