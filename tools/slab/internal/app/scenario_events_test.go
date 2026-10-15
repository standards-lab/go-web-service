package app_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

// eventsFake stands in for the service's organization and document
// surfaces for the events scenario on one listener: any number of
// organizations, each with a logo and a document tree of its own, the root
// created by the organization's first document write. It answers the way
// the service does, a problem document for every refusal, and logs the
// event type each successful command would publish under the README's
// Messaging rules, so a test can check that the scenario's sequence makes
// the events its narration names. fault names one request line ("METHOD
// path") the fake answers 500 instead, and deletable lets an organization
// with documents or a logo history be deleted, for the unexpected-response
// tests.
type eventsFake struct {
	mu        sync.Mutex
	orgs      map[string]*eventsOrg
	nodes     map[string]*fakeNode
	deleting  map[string]int
	serial    int
	requests  []string
	emitted   []string
	fault     string
	deletable bool
	err       error
}

// eventsOrg is one organization the fake holds: its row, its document
// root's id once a document write created it, and whether it ever had a
// logo, whose history references it as the root does.
type eventsOrg struct {
	row     organization.Organization
	root    string
	logo    []byte
	hadLogo bool
}

func newEventsFake() *eventsFake {
	f := &eventsFake{orgs: map[string]*eventsOrg{}, nodes: map[string]*fakeNode{}, deleting: map[string]int{}}
	f.orgs[fakeAcmeID] = &eventsOrg{row: organization.Organization{ID: fakeAcmeID, Code: "acme", Name: "Acme", Version: 1, Path: acmePath}}
	return f
}

func (f *eventsFake) newID() string {
	f.serial++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", f.serial)
}

func (f *eventsFake) emit(kind string) { f.emitted = append(f.emitted, "web-service."+kind) }

func (f *eventsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := r.Method + " " + r.URL.RequestURI()
	if v := r.Header.Get("If-Match"); v != "" {
		line += " If-Match: " + v
	}
	f.requests = append(f.requests, line)
	w.Header().Set("X-Request-Id", fakeTrace)
	if f.fault != "" && r.Method+" "+r.URL.Path == f.fault {
		writeProblem(w, http.StatusInternalServerError, "")
		return
	}
	f.route(w, r)
}

// fail records a request no route answers, for the test to report.
func (f *eventsFake) fail(w http.ResponseWriter, r *http.Request) {
	if f.err == nil {
		f.err = fmt.Errorf("no route for %s %s", r.Method, r.URL.RequestURI())
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (f *eventsFake) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/readyz":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && p == organization.Organizations+"/lookup":
		for _, o := range f.orgs {
			if o.row.Path == r.URL.Query().Get("path") {
				writeJSON(w, http.StatusOK, o.row)
				return
			}
		}
		writeProblem(w, http.StatusNotFound, "")
	case r.Method == http.MethodPost && p == organization.Organizations:
		f.createOrg(w, r)
	case strings.HasPrefix(p, organization.Organizations+"/"):
		seg := strings.Split(strings.TrimPrefix(p, organization.Organizations+"/"), "/")
		o := f.orgs[seg[0]]
		if o == nil {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.serveOrg(w, r, o, seg[1:])
	case strings.HasPrefix(p, document.Documents+"/"):
		seg := strings.Split(strings.TrimPrefix(p, document.Documents+"/"), "/")
		o := f.orgs[seg[0]]
		if o == nil {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.serveDocuments(w, r, o, seg[1:])
	default:
		f.fail(w, r)
	}
}

// createOrg validates the code and refuses one a sibling carries, as the
// unique constraint does.
func (f *eventsFake) createOrg(w http.ResponseWriter, r *http.Request) {
	var body organization.CreateOrganization
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.fail(w, r)
		return
	}
	if !codePattern.MatchString(body.Code) || body.Name == "" {
		writeProblem(w, http.StatusBadRequest, "")
		return
	}
	for _, o := range f.orgs {
		if o.row.Code == body.Code && sameParent(o.row.ParentID, body.ParentID) {
			writeProblem(w, http.StatusConflict, "")
			return
		}
	}
	o := &eventsOrg{row: organization.Organization{ID: f.newID(), ParentID: body.ParentID, Code: body.Code, Name: body.Name, Version: 1}}
	f.orgs[o.row.ID] = o
	f.emit("organization.created")
	writeJSON(w, http.StatusCreated, organization.Identity{ID: o.row.ID, Version: 1})
}

func (f *eventsFake) serveOrg(w http.ResponseWriter, r *http.Request, o *eventsOrg, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, o.row)
	case len(rest) == 0 && r.Method == http.MethodPut:
		var body organization.EditOrganization
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		if storageIfMatch(w, r, o.row.Version) {
			o.row.Code, o.row.Name, o.row.Version = body.Code, body.Name, o.row.Version+1
			f.emit("organization.edited")
			writeJSON(w, http.StatusOK, organization.Identity{ID: o.row.ID, Version: o.row.Version})
		}
	case len(rest) == 1 && rest[0] == "transfer" && r.Method == http.MethodPost:
		var body organization.TransferOrganization
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		if storageIfMatch(w, r, o.row.Version) {
			o.row.ParentID, o.row.Version = body.ParentID, o.row.Version+1
			f.emit("organization.transferred")
			writeJSON(w, http.StatusOK, organization.Identity{ID: o.row.ID, Version: o.row.Version})
		}
	case len(rest) == 0 && r.Method == http.MethodDelete:
		if !storageIfMatch(w, r, o.row.Version) {
			return
		}
		if !f.deletable && (o.root != "" || o.hadLogo) {
			writeProblem(w, http.StatusConflict, "the request conflicts with the current state")
			return
		}
		delete(f.orgs, o.row.ID)
		f.emit("organization.deleted")
		w.WriteHeader(http.StatusNoContent)
	case len(rest) == 1 && rest[0] == "logo" && r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if !strings.HasPrefix(string(body), "\x89PNG") {
			writeProblem(w, http.StatusUnsupportedMediaType, "")
			return
		}
		o.logo, o.hadLogo = body, true
		f.emit("organization.logo.uploaded")
		writeJSON(w, http.StatusCreated, map[string]string{"id": f.newID()})
	case len(rest) == 1 && rest[0] == "logo" && r.Method == http.MethodDelete:
		if o.logo == nil {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		o.logo = nil
		f.emit("organization.logo.deleted")
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, r)
	}
}

// resolve finds id, or the root alias, under o's root; a write creates the
// root when it is absent and create is set, as the first document write
// does, publishing its directory.created.
func (f *eventsFake) resolve(o *eventsOrg, id string, create bool) *fakeNode {
	if o.root == "" && create && id == document.RootAlias {
		o.root = f.newID()
		f.nodes[o.root] = &fakeNode{id: o.root, name: "/", dir: true, status: document.DirectoryActive, version: 1}
		f.emit("document.directory.created")
	}
	if o.root == "" {
		return nil
	}
	if id == document.RootAlias {
		return f.nodes[o.root]
	}
	n := f.nodes[id]
	for c := n; c != nil; c = f.nodes[c.parent] {
		if c.id == o.root {
			return n
		}
	}
	return nil
}

func (f *eventsFake) children(parent string) []*fakeNode {
	var out []*fakeNode
	for _, n := range f.nodes {
		if n.parent == parent {
			out = append(out, n)
		}
	}
	return out
}

func (f *eventsFake) serveDocuments(w http.ResponseWriter, r *http.Request, o *eventsOrg, seg []string) {
	switch {
	case len(seg) == 1 && seg[0] == "directories" && r.Method == http.MethodPost:
		var body document.CreateDirectory
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		parent := f.resolve(o, body.ParentID, true)
		if parent == nil || !parent.dir {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		n := &fakeNode{id: f.newID(), parent: parent.id, name: body.Name, dir: true, status: document.DirectoryActive, version: 1}
		f.nodes[n.id] = n
		f.emit("document.directory.created")
		writeJSON(w, http.StatusCreated, document.Identity{ID: n.id, Version: n.version})
	case len(seg) >= 2 && seg[0] == "directories":
		n := f.resolve(o, seg[1], false)
		if n == nil || !n.dir {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.serveDirectory(w, r, o, n, seg[2:])
	case len(seg) >= 2 && seg[0] == "files":
		n := f.resolve(o, seg[1], false)
		if n == nil || n.dir {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.serveFile(w, r, o, n, seg[2:])
	default:
		f.fail(w, r)
	}
}

func (f *eventsFake) serveDirectory(w http.ResponseWriter, r *http.Request, o *eventsOrg, n *fakeNode, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		if left, ok := f.deleting[n.id]; ok {
			if left == 0 {
				f.sweep(n.id)
				writeProblem(w, http.StatusNotFound, "")
				return
			}
			f.deleting[n.id] = left - 1
		}
		parent := n.parent
		writeJSON(w, http.StatusOK, document.Directory{ID: n.id, ParentID: &parent, Name: n.name, Status: n.status, Version: n.version})
	case len(rest) == 0 && r.Method == http.MethodDelete:
		if !storageIfMatch(w, r, n.version) {
			return
		}
		if r.URL.Query().Get("recursive") == "true" {
			n.status, n.version = document.DirectoryDeleting, n.version+1
			f.deleting[n.id] = sweepReads
			f.emit("document.directory.deleted")
			w.Header().Set("Location", r.URL.Path)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if len(f.children(n.id)) > 0 {
			writeProblem(w, http.StatusConflict, "")
			return
		}
		delete(f.nodes, n.id)
		f.emit("document.directory.deleted")
		w.WriteHeader(http.StatusNoContent)
	case len(rest) == 1 && rest[0] == "move" && r.Method == http.MethodPost:
		var body document.MoveDirectory
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		f.moveNode(w, r, o, n, body.ParentID, body.Name, "document.directory.moved")
	case len(rest) == 1 && rest[0] == "files" && r.Method == http.MethodPost && r.URL.Query().Get("name") != "":
		if _, err := io.ReadAll(r.Body); err != nil {
			f.fail(w, r)
			return
		}
		file := &fakeNode{id: f.newID(), parent: n.id, name: r.URL.Query().Get("name"), contentType: r.Header.Get("Content-Type"), status: "available", version: 2}
		f.nodes[file.id] = file
		f.emit("document.file.uploaded")
		writeJSON(w, http.StatusCreated, document.Identity{ID: file.id, Version: file.version})
	default:
		f.fail(w, r)
	}
}

func (f *eventsFake) serveFile(w http.ResponseWriter, r *http.Request, o *eventsOrg, n *fakeNode, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodDelete:
		if storageIfMatch(w, r, n.version) {
			delete(f.nodes, n.id)
			f.emit("document.file.deleted")
			w.WriteHeader(http.StatusNoContent)
		}
	case len(rest) == 1 && rest[0] == "move" && r.Method == http.MethodPost:
		var body document.MoveFile
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		f.moveNode(w, r, o, n, body.DirectoryID, body.Name, "document.file.moved")
	default:
		f.fail(w, r)
	}
}

// moveNode moves n under the directory parent as name, guarded at n's
// version, and publishes kind.
func (f *eventsFake) moveNode(w http.ResponseWriter, r *http.Request, o *eventsOrg, n *fakeNode, parent, name, kind string) {
	if !storageIfMatch(w, r, n.version) {
		return
	}
	dest := f.resolve(o, parent, false)
	if dest == nil || !dest.dir {
		writeProblem(w, http.StatusNotFound, "")
		return
	}
	n.parent, n.name, n.version = dest.id, name, n.version+1
	f.emit(kind)
	writeJSON(w, http.StatusOK, document.Identity{ID: n.id, Version: n.version})
}

// sweep removes the branch at id, publishing nothing, as the service's
// sweep does.
func (f *eventsFake) sweep(id string) {
	for _, c := range f.children(id) {
		f.sweep(c.id)
	}
	delete(f.nodes, id)
	delete(f.deleting, id)
}

// runEventsScenario runs the events scenario against fake and returns the
// run; a request no route answers fails the test.
func runEventsScenario(t *testing.T, fake *eventsFake) scenarioRun {
	t.Helper()
	return runOrgScenario(t, "events", fake, func() error { return fake.err })
}

// runEventsOK is runEventsScenario expecting the run to succeed, and
// returns its narration.
func runEventsOK(t *testing.T, fake *eventsFake) string {
	t.Helper()
	r := runEventsScenario(t, fake)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	return r.stdout
}

// eventsSteps is the scenario's headings in order.
var eventsSteps = []string{
	"[1/16] Organization Creation", "[2/16] Organization Edit", "[3/16] Organization Transfer",
	"[4/16] Logo Upload", "[5/16] Logo Replacement", "[6/16] Logo Delete",
	"[7/16] First Directory", "[8/16] More Directories", "[9/16] File Uploads",
	"[10/16] Directory Move", "[11/16] File Move", "[12/16] File Delete",
	"[13/16] Empty Directory Delete", "[14/16] Branch Delete", "[15/16] Last Directory Delete",
	"[16/16] Organization Delete",
}

// eventsByStep is the event types each step's commands publish under the
// README's rules, in order: the twenty the scenario makes in all.
var eventsByStep = [][]string{
	{"organization.created"},
	{"organization.edited"},
	{"organization.transferred"},
	{"organization.logo.uploaded"},
	{"organization.logo.uploaded"},
	{"organization.logo.deleted"},
	{"document.directory.created", "document.directory.created"},
	{"document.directory.created", "document.directory.created"},
	{"document.file.uploaded", "document.file.uploaded"},
	{"document.directory.moved"},
	{"document.file.moved"},
	{"document.file.deleted"},
	{"document.directory.deleted"},
	{"document.directory.deleted"},
	{"document.directory.deleted"},
	{"organization.created", "organization.deleted"},
}

// The events scenario builds the service node, with the base setting it
// reads, then the settings its steps read; it points at no trace, so it
// neither builds nor probes grafana up.
func TestScenario_EventsRunsSixteenStepsInOrder(t *testing.T) {
	fake := newEventsFake()
	r := runEventsScenario(t, fake)
	if r.code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitOK, r.stderr, r.stdout)
	}
	inOrder(t, r.stdout, eventsSteps)
	if want := []string{"service", "base", "client", "repo", "no color", "lifecycle config"}; !slices.Equal(r.built, want) {
		t.Errorf("nodes built = %q, want %q", r.built, want)
	}
	if slices.Contains(fake.requests, "GET /api/health") {
		t.Errorf("the run probed Grafana; sent:\n%s", strings.Join(fake.requests, "\n"))
	}
}

// The scenario's commands make every event type the service publishes, in
// the order and number its steps name, twenty in all.
func TestScenario_EventsMakesEveryEventTypeInOrder(t *testing.T) {
	fake := newEventsFake()
	runEventsOK(t, fake)
	var want []string
	for _, kinds := range eventsByStep {
		for _, k := range kinds {
			want = append(want, "web-service."+k)
		}
	}
	if !slices.Equal(fake.emitted, want) {
		t.Errorf("the run made the events\n%s\nwant\n%s", strings.Join(fake.emitted, "\n"), strings.Join(want, "\n"))
	}
	if len(want) != 20 {
		t.Errorf("the steps make %d events, want the twenty the closing note states", len(want))
	}
	types := map[string]bool{}
	for _, k := range want {
		types[k] = true
	}
	if len(types) != 12 {
		t.Errorf("the run makes %d event types, want all 12 the service publishes", len(types))
	}
}

// Each step's narration names every event type its commands make, so the
// reader knows what the watcher should print.
func TestScenario_EventsNamesEachStepsEvents(t *testing.T) {
	out := runEventsOK(t, newEventsFake())
	for i, heading := range eventsSteps {
		flat := strings.Join(strings.Fields(step(t, out, heading[:strings.Index(heading, " ")])), " ")
		for _, k := range eventsByStep[i] {
			if !strings.Contains(flat, "web-service."+k) {
				t.Errorf("step %s does not name web-service.%s:\n%s", heading, k, flat)
			}
		}
	}
}

// The first step tells the reader to start the watcher, by the command the
// README gives, and the last says the run's first organization stays.
func TestScenario_EventsOpensWithTheWatcherAndClosesWithWhatItLeaves(t *testing.T) {
	out := runEventsOK(t, newEventsFake())
	first := strings.Join(strings.Fields(step(t, out, "[1/16]")), " ")
	if !strings.Contains(first, `Run "mise run slab -- events watch" in another terminal first`) {
		t.Errorf("the first step does not open with the watcher:\n%s", first)
	}
	last := strings.Join(strings.Fields(step(t, out, "[16/16]")), " ")
	for _, want := range []string{"HTTP 409 Conflict", "That is all 20 events", "behind at the root, its document root empty"} {
		if !strings.Contains(last, want) {
			t.Errorf("the last step lacks %q:\n%s", want, last)
		}
	}
}

// The logo steps upload two different seeded fixtures, their request bodies
// summarized rather than printed.
func TestScenario_EventsUploadsTwoFixturesSummarized(t *testing.T) {
	out := runEventsOK(t, newEventsFake())
	for heading, fixture := range map[string]string{"[4/16]": "operations.png", "[5/16]": "product.png"} {
		if s := step(t, out, heading); !strings.Contains(s, "bytes of data/seeds/fixtures/"+fixture+")") {
			t.Errorf("step %s does not summarize %s:\n%s", heading, fixture, s)
		}
	}
}

// Every name the run creates is its own, so a second run against the same
// fake, which refuses a taken code, succeeds, and leaves a second
// organization behind beside the first.
func TestScenario_EventsRunsTwiceAgainstTheSameFake(t *testing.T) {
	fake := newEventsFake()
	for run := 1; run <= 2; run++ {
		t.Logf("run %d", run)
		runEventsOK(t, fake)
	}
	if got := len(fake.orgs); got != 3 {
		t.Errorf("the runs left %d organizations, want acme and one per run", got)
	}
	if got := len(fake.emitted); got != 40 {
		t.Errorf("the runs made %d events, want twenty each", got)
	}
}

// A run fails at the step whose response is not the one it expects, naming
// the step, and goes no further: an unexpected status, and an organization
// delete the service does not refuse.
func TestScenario_EventsFailsNamingTheStep(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*eventsFake)
		want  string
		next  string
	}{
		"a failed logo delete": {
			// The run's organization is the first id the fake mints.
			func(f *eventsFake) {
				f.fault = http.MethodDelete + " " + organization.Organizations + "/00000000-0000-7000-8000-000000000001/logo"
			},
			"slab scenario events: step 6 (Logo Delete): status = 500", "[7/16]",
		},
		"an organization delete not refused": {
			func(f *eventsFake) { f.deletable = true },
			"slab scenario events: step 16 (Organization Delete): status = 204", "That is all",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newEventsFake()
			tc.setup(fake)
			r := runEventsScenario(t, fake)
			if r.code != process.ExitFailure {
				t.Fatalf("code = %d, want %d; stderr = %q\n%s", r.code, process.ExitFailure, r.stderr, r.stdout)
			}
			if !strings.HasPrefix(r.stderr, tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, tc.want)
			}
			if n := strings.Count(r.stderr, "slab scenario"); n != 1 {
				t.Errorf("stderr reports %d times, want once:\n%s", n, r.stderr)
			}
			if strings.Contains(r.stdout, tc.next) {
				t.Errorf("the run went on past the failed step:\n%s", r.stdout)
			}
		})
	}
}

// The recursive deletes wait out the sweep, which the fake runs after
// sweepReads reads of the marked directory, so each read is sent and the
// branch with its file is gone.
func TestScenario_EventsWaitsOutEachSweep(t *testing.T) {
	fake := newEventsFake()
	out := runEventsOK(t, fake)
	for _, heading := range []string{"[14/16]", "[15/16]"} {
		if !strings.Contains(step(t, out, heading), "  The sweep removed ") {
			t.Errorf("step %s does not report the wait:\n%s", heading, out)
		}
	}
	for _, n := range fake.nodes {
		if n.parent != "" {
			t.Errorf("the run left %s under the root", n.name)
		}
	}
	if got := len(fake.nodes); got != 1 {
		t.Errorf("the run left %d document nodes, want the root alone", got)
	}
}
