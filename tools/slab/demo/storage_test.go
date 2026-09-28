package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// storageFake stands in for the service's storage surface on one listener:
// the schema status, the additive seed and the reset, the organization
// lookup, acme's logo, and acme's document tree, seeded from the real
// data/seeds/default.json. It answers the way the service does, a problem
// document for every refusal, and records each request's line with its
// If-Match or If-None-Match so a test can check what the steps sent.
//
// A recursive delete marks the branch, and the branch's read answers
// deleting for sweepReads reads before the fake sweeps it, so the scenario
// sees the deleting status and the wait polls at least once. fault names
// one request line ("METHOD path") the fake answers 500 instead, and
// takenDetail replaces a taken name's curated detail, for the
// unexpected-response tests.
type storageFake struct {
	mu          sync.Mutex
	seed        seedFile
	logo        []byte
	logoTag     int
	nodes       map[string]*fakeNode
	deleting    map[string]int
	serial      int
	requests    []string
	fault       string
	takenDetail string
	err         error
}

// fakeNode is one directory or file under acme's root; the root itself is a
// node with no parent.
type fakeNode struct {
	id, parent, name, contentType, status string
	dir                                   bool
	version                               int64
}

// The organizations the fake's lookup knows, and the seeded logo's bytes:
// any bytes do, since the scenario only reads them back.
var (
	fakeAcmeID    = fakeID(1)
	fakeFinanceID = fakeID(7)
	fakeSeedLogo  = []byte("\x89PNG\r\n\x1a\nseeded")
)

// sweepReads is how many reads of a marked branch answer deleting.
const sweepReads = 2

func newStorageFake(t *testing.T) *storageFake {
	t.Helper()
	seed, err := loadSeed(context.Background())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	f := &storageFake{seed: seed, takenDetail: detailNameTaken}
	f.reseed()
	return f
}

// reseed restores the seeded logo and acme's seeded tree, as a reset does.
func (f *storageFake) reseed() {
	f.logo, f.logoTag = fakeSeedLogo, f.logoTag+1
	f.nodes, f.deleting = map[string]*fakeNode{}, map[string]int{}
	tree, _ := f.seed.tree(storageOrgPath)
	f.nodes[tree.Root] = &fakeNode{id: tree.Root, name: "/", dir: true, status: document.DirectoryActive, version: 1}
	var add func(parent string, entries []seedEntry)
	add = func(parent string, entries []seedEntry) {
		for _, e := range entries {
			n := &fakeNode{id: e.ID, parent: parent, name: e.Name, contentType: e.ContentType, dir: e.ContentType == "", status: document.DirectoryActive, version: 1}
			if !n.dir {
				n.status, n.version = "available", 2
			}
			f.nodes[e.ID] = n
			add(e.ID, e.Entries)
		}
	}
	add(tree.Root, tree.Entries)
}

func (f *storageFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := r.Method + " " + r.URL.RequestURI()
	for _, h := range []string{"If-Match", "If-None-Match"} {
		if v := r.Header.Get(h); v != "" {
			line += " " + h + ": " + v
		}
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
func (f *storageFake) fail(w http.ResponseWriter, r *http.Request) {
	if f.err == nil {
		f.err = fmt.Errorf("no route for %s %s", r.Method, r.URL.RequestURI())
	}
	w.WriteHeader(http.StatusBadRequest)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", web.ProblemMediaType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(web.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
}

// fakeSchema is the schema status the fake answers, the service's two sets
// at their latest.
var fakeSchema = map[string]any{
	"ready": true,
	"sets": []map[string]any{
		{"name": "blobfs", "version": 3, "latest": 3, "dirty": false},
		{"name": "app", "version": 4, "latest": 4, "dirty": false},
	},
}

func (f *storageFake) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	logo := organization.Organizations + "/" + fakeAcmeID + "/logo"
	switch {
	case r.Method == http.MethodGet && p == "/healthz":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && p == schemaRoute:
		writeJSON(w, http.StatusOK, fakeSchema)
	case r.Method == http.MethodPost && p == seedRoute:
		n := seeded{}
		if f.logo == nil {
			f.logo, f.logoTag, n.Logos = fakeSeedLogo, f.logoTag+1, 1
		}
		writeJSON(w, http.StatusOK, n)
	case r.Method == http.MethodPost && p == stateRoute:
		f.reseed()
		writeJSON(w, http.StatusOK, map[string]any{"state": SeedState, "schema": fakeSchema, "seeded": f.seed.seedCounts()})
	case r.Method == http.MethodGet && p == organization.Organizations+"/lookup":
		switch r.URL.Query().Get("path") {
		case storageOrgPath:
			writeJSON(w, http.StatusOK, organization.Organization{ID: fakeAcmeID, Code: "acme", Version: 1, Path: storageOrgPath})
		case otherOrgPath:
			writeJSON(w, http.StatusOK, organization.Organization{ID: fakeFinanceID, Code: "finance", Version: 1, Path: otherOrgPath})
		default:
			writeProblem(w, http.StatusNotFound, "")
		}
	case p == logo:
		f.serveLogo(w, r)
	case strings.HasPrefix(p, document.Documents+"/"):
		f.serveDocuments(w, r, strings.Split(strings.TrimPrefix(p, document.Documents+"/"), "/"))
	default:
		f.fail(w, r)
	}
}

func (f *storageFake) serveLogo(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if f.logo == nil {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		etag := `"0x` + strconv.Itoa(f.logoTag) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Mon, 28 Sep 2026 16:00:00 GMT")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.logo)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.logo, f.logoTag = body, f.logoTag+1
		w.Header().Set("Location", r.URL.Path)
		writeJSON(w, http.StatusCreated, map[string]string{"id": f.newID()})
	case http.MethodDelete:
		f.logo = nil
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, r)
	}
}

func (f *storageFake) newID() string {
	f.serial++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", f.serial)
}

// root is acme's root node.
func (f *storageFake) root() *fakeNode {
	for _, n := range f.nodes {
		if n.parent == "" {
			return n
		}
	}
	return nil
}

// resolve finds id, or the root alias, under org's root: only acme has one,
// so any id under another organization is not found, as the scope check
// answers.
func (f *storageFake) resolve(org, id string) *fakeNode {
	if org != fakeAcmeID {
		return nil
	}
	if id == document.RootAlias {
		return f.root()
	}
	return f.nodes[id]
}

// inDeleting reports whether n or a directory above it is marked.
func (f *storageFake) inDeleting(n *fakeNode) bool {
	for ; n != nil; n = f.nodes[n.parent] {
		if n.status == document.DirectoryDeleting {
			return true
		}
	}
	return false
}

func (f *storageFake) children(parent string, dirs bool) []*fakeNode {
	var out []*fakeNode
	for _, n := range f.nodes {
		if n.parent == parent && n.dir == dirs {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func (f *storageFake) dirView(n *fakeNode) document.Directory {
	d := document.Directory{ID: n.id, Name: n.name, Status: n.status, Version: n.version}
	if n.parent != "" {
		parent := n.parent
		d.ParentID = &parent
	}
	return d
}

func (f *storageFake) fileView(n *fakeNode) document.File {
	size := int64(1)
	return document.File{ID: n.id, DirectoryID: n.parent, Name: n.name, Status: n.status, Size: &size, ContentType: n.contentType, Version: n.version}
}

// ifMatch reads the request's If-Match as the version it quotes, answering
// 428 when it is missing and 412 when it is not want.
func ifMatch(w http.ResponseWriter, r *http.Request, want int64) bool {
	v := r.Header.Get("If-Match")
	if v == "" {
		writeProblem(w, http.StatusPreconditionRequired, "the request requires an If-Match header")
		return false
	}
	if n, err := strconv.ParseInt(strings.Trim(v, `"`), 10, 64); err != nil || n != want {
		writeProblem(w, http.StatusPreconditionFailed, "")
		return false
	}
	return true
}

func (f *storageFake) serveDocuments(w http.ResponseWriter, r *http.Request, seg []string) {
	org := seg[0]
	switch {
	case len(seg) == 2 && seg[1] == "directories" && r.Method == http.MethodPost:
		var body document.CreateDirectory
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, r)
			return
		}
		parent := f.resolve(org, body.ParentID)
		if parent == nil {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		for _, c := range f.children(parent.id, true) {
			if c.name == body.Name {
				writeProblem(w, http.StatusConflict, f.takenDetail)
				return
			}
		}
		n := &fakeNode{id: f.newID(), parent: parent.id, name: body.Name, dir: true, status: document.DirectoryActive, version: 1}
		f.nodes[n.id] = n
		writeJSON(w, http.StatusCreated, document.Identity{ID: n.id, Version: n.version})
	case len(seg) >= 3 && seg[1] == "directories":
		n := f.resolve(org, seg[2])
		if n == nil || !n.dir {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.serveDirectory(w, r, n, seg[3:])
	case len(seg) == 3 && seg[1] == "files":
		n := f.resolve(org, seg[2])
		if n == nil || n.dir {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, f.fileView(n))
		case http.MethodDelete:
			if ifMatch(w, r, n.version) {
				delete(f.nodes, n.id)
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			f.fail(w, r)
		}
	default:
		f.fail(w, r)
	}
}

func (f *storageFake) serveDirectory(w http.ResponseWriter, r *http.Request, n *fakeNode, rest []string) {
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
		writeJSON(w, http.StatusOK, f.dirView(n))
	case len(rest) == 0 && r.Method == http.MethodDelete:
		if r.URL.Query().Get("recursive") != "true" {
			f.fail(w, r)
			return
		}
		if !ifMatch(w, r, n.version) {
			return
		}
		n.status, n.version = document.DirectoryDeleting, n.version+1
		f.deleting[n.id] = sweepReads
		w.Header().Set("Location", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	case len(rest) == 1 && r.Method == http.MethodGet && (rest[0] == "directories" || rest[0] == "files"):
		if f.inDeleting(n) {
			writeProblem(w, http.StatusNotFound, "")
			return
		}
		f.list(w, r, n, rest[0] == "directories")
	case len(rest) == 2 && rest[0] == "files" && r.Method == http.MethodPut:
		if f.inDeleting(n) {
			writeProblem(w, http.StatusConflict, "the directory is being deleted")
			return
		}
		file := &fakeNode{id: f.newID(), parent: n.id, name: rest[1], contentType: r.Header.Get("Content-Type"), status: "available", version: 2}
		f.nodes[file.id] = file
		writeJSON(w, http.StatusCreated, document.Identity{ID: file.id, Version: file.version})
	default:
		f.fail(w, r)
	}
}

// sweep removes the branch at id, as the service's sweep does.
func (f *storageFake) sweep(id string) {
	for _, c := range f.nodes {
		if c.parent == id {
			f.sweep(c.id)
		}
	}
	delete(f.nodes, id)
	delete(f.deleting, id)
}

// list answers a child listing sorted by name, size to a page, continued
// after the name its cursor names; a continued page carries no page number.
func (f *storageFake) list(w http.ResponseWriter, r *http.Request, n *fakeNode, dirs bool) {
	q := r.URL.Query()
	if s := q.Get("sort"); s != "" && s != "name" {
		f.fail(w, r)
		return
	}
	size := document.DefaultPageSize
	if s := q.Get("size"); s != "" {
		size, _ = strconv.Atoi(s)
	}
	all := f.children(n.id, dirs)
	total := len(all)
	rows, page := all, 1
	if c := q.Get("cursor"); c != "" {
		after := strings.TrimPrefix(c, "after:")
		rows, page = nil, 0
		for _, n := range all {
			if n.name > after {
				rows = append(rows, n)
			}
		}
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	next := ""
	if more {
		next = "after:" + rows[len(rows)-1].name
	}
	if dirs {
		p := document.DirectoryPage{Page: page, Size: size, Total: &total, More: more, Next: next, Items: []document.Directory{}}
		for _, n := range rows {
			p.Items = append(p.Items, f.dirView(n))
		}
		writeJSON(w, http.StatusOK, p)
		return
	}
	p := document.FilePage{Page: page, Size: size, Total: &total, More: more, Next: next, Items: []document.File{}}
	for _, n := range rows {
		p.Items = append(p.Items, f.fileView(n))
	}
	writeJSON(w, http.StatusOK, p)
}

// runStorage runs the storage scenario against fake and returns its error
// and output; the fake's own error, a request no route answers, fails the
// test.
func runStorage(t *testing.T, fake *storageFake) (string, error) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	ctx := env.WithContext(context.Background(), env.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
	var out bytes.Buffer
	err := scenario.Run(ctx, Storage(), scenario.NewReporter(&out, false))
	srv.Close()
	if fake.err != nil {
		t.Fatalf("the scenario sent a bad request: %v\n%s", fake.err, out.String())
	}
	return out.String(), err
}

// storageSteps is the scenario's headings in order.
var storageSteps = []string{
	"[1/16] Migration Sets", "[2/16] Additive Seed", "[3/16] Logo Read",
	"[4/16] Logo Revalidation", "[5/16] Logo Replacement", "[6/16] Logo Delete",
	"[7/16] Document Tree", "[8/16] Cursor Walk", "[9/16] Missing If-Match",
	"[10/16] Stale If-Match", "[11/16] Taken Name", "[12/16] Another Organization",
	"[13/16] Branch Creation", "[14/16] Recursive Delete", "[15/16] Sweep", "[16/16] Reset",
}

func TestStorage_RunsSixteenStepsInOrderAgainstTheFake(t *testing.T) {
	out, err := runStorage(t, newStorageFake(t))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	last := -1
	for _, s := range storageSteps {
		at := strings.Index(out, s)
		if at < 0 {
			t.Fatalf("output lacks %q:\n%s", s, out)
		}
		if at < last {
			t.Errorf("%q is out of order", s)
		}
		last = at
	}
	if strings.Contains(out, "[17/") {
		t.Errorf("the scenario narrates more than sixteen steps:\n%s", out)
	}
}

func TestStorage_SendsTheScenarioSequence(t *testing.T) {
	fake := newStorageFake(t)
	out, err := runStorage(t, fake)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	tree, _ := fake.seed.tree(storageOrgPath)
	paths := tree.paths()
	logo := organization.Organizations + "/" + fakeAcmeID + "/logo"
	docs := document.Documents + "/" + fakeAcmeID
	readme := docs + "/files/" + paths[guardedFilePath]
	// Each entry is one request the fake recorded, in the order the steps
	// send them, matched whole or, where prefix is set, by its start;
	// requests between them are not checked.
	want := []struct {
		line   string
		prefix bool
	}{
		{"GET " + schemaRoute, false},
		{"POST " + seedRoute, false},
		{"GET " + logo, false},
		{"GET " + logo + ` If-None-Match: "0x1"`, false},
		{"PUT " + logo, false},
		{"GET " + logo, false},
		{"DELETE " + logo, false},
		{"GET " + logo, false},
		{"GET " + docs + "/directories/root/directories?sort=name", false},
		{"GET " + docs + "/directories/root/files?sort=name", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name&cursor=after%3Aengineering", false},
		{"GET " + docs + "/directories/root/directories?size=1&sort=name&cursor=after%3Afinance", false},
		{"GET " + readme, false},
		{"DELETE " + readme, false},
		{"DELETE " + readme + ` If-Match: "1"`, false},
		{"POST " + docs + "/directories", false},
		{"GET " + docs + "/directories/" + paths[takenDirPath+"/"], false},
		{"GET " + document.Documents + "/" + fakeFinanceID + "/directories/" + paths[takenDirPath+"/"], false},
		{"POST " + docs + "/directories", false},
		{"POST " + docs + "/directories", false},
		{"PUT " + docs + "/directories/", true},
		{"DELETE " + docs + "/directories/", true},
		{"POST " + stateRoute, false},
	}
	i := 0
	for _, got := range fake.requests {
		if i == len(want) {
			break
		}
		if w := want[i]; got == w.line || (w.prefix && strings.HasPrefix(got, w.line)) {
			i++
		}
	}
	if i < len(want) {
		t.Errorf("the requests lack %q in order; sent:\n%s", want[i].line, strings.Join(fake.requests, "\n"))
	}
	if !strings.Contains(strings.Join(fake.requests, "\n"), `?recursive=true If-Match: "1"`) {
		t.Errorf("the recursive delete did not guard on the branch's version; sent:\n%s", strings.Join(fake.requests, "\n"))
	}
}

func TestStorage_LeavesTheFakeAsTheResetSeededIt(t *testing.T) {
	fake := newStorageFake(t)
	if _, err := runStorage(t, fake); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fake.logo, fakeSeedLogo) {
		t.Error("the run left acme without its seeded logo")
	}
	tree, _ := fake.seed.tree(storageOrgPath)
	if got, want := len(fake.nodes), len(tree.paths())+1; got != want {
		t.Errorf("the run left %d nodes, want the seeded %d", got, want)
	}
}

func TestStorage_RunsTwiceAgainstTheSameFake(t *testing.T) {
	fake := newStorageFake(t)
	for run := 1; run <= 2; run++ {
		if out, err := runStorage(t, fake); err != nil {
			t.Fatalf("run %d: %v\n%s", run, err, out)
		}
	}
}

func TestStorage_RestoresADeletedLogoThroughTheAdditiveSeed(t *testing.T) {
	fake := newStorageFake(t)
	fake.logo = nil // an earlier run stopped after its logo delete
	out, err := runStorage(t, fake)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(step(t, out, "[2/16]"), `"logos": 1`) {
		t.Errorf("the additive seed does not report the logo it wrote again:\n%s", out)
	}
}

func TestStorage_FailsNamingTheStepOnAnUnexpectedResponse(t *testing.T) {
	fake := newStorageFake(t)
	fake.fault = http.MethodDelete + " " + organization.Organizations + "/" + fakeAcmeID + "/logo"
	out, err := runStorage(t, fake)
	if err == nil {
		t.Fatalf("run = nil, want the logo delete's failure:\n%s", out)
	}
	if want := "storage: step 6 (Logo Delete): status = 500"; !strings.Contains(err.Error(), want) {
		t.Errorf("run error = %v; want it to contain %q", err, want)
	}
	if strings.Contains(out, "[7/16]") {
		t.Errorf("the run went on past the failed step:\n%s", out)
	}
}

func TestStorage_FailsWhenTheConflictDetailIsNotTheCuratedOne(t *testing.T) {
	fake := newStorageFake(t)
	fake.takenDetail = "the request conflicts with the current state"
	out, err := runStorage(t, fake)
	if err == nil || !strings.Contains(err.Error(), "storage: step 11 (Taken Name): the conflict's detail is") {
		t.Errorf("run error = %v; want the taken name's detail check to fail:\n%s", err, out)
	}
}

func TestStorage_FailsWhenASeededEntryIsMissingFromTheTree(t *testing.T) {
	fake := newStorageFake(t)
	fake.nodes[fake.seed.Documents[0].Entries[0].ID].name = "renamed.txt"
	out, err := runStorage(t, fake)
	if err == nil || !strings.Contains(err.Error(), "storage: step 7 (Document Tree): the tree has /README.txt") {
		t.Errorf("run error = %v; want the tree check to name the missing entry:\n%s", err, out)
	}
}

func TestStorage_NarratesEachStepAsNoteRequestResponse(t *testing.T) {
	out, err := runStorage(t, newStorageFake(t))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	logo := organization.Organizations + "/" + fakeAcmeID + "/logo"
	for _, c := range []struct{ heading, note, request, status string }{
		{"[1/16]", "two migration sets", "GET " + schemaRoute, "HTTP 200 OK"},
		{"[2/16]", "A seed is additive", "POST " + seedRoute, "HTTP 200 OK"},
		{"[3/16]", "served by proxy", "GET " + logo, "HTTP 200 OK"},
		{"[4/16]", "If-None-Match", "GET " + logo, "HTTP 304 Not Modified"},
		{"[5/16]", "takes no version", "PUT " + logo, "HTTP 201 Created"},
		{"[6/16]", "retires whatever logo", "DELETE " + logo, "HTTP 204 No Content"},
		{"[7/16]", "hang from its own root", "GET " + document.Documents, "HTTP 200 OK"},
		{"[8/16]", "next cursor", "GET " + document.Documents, "HTTP 200 OK"},
		{"[9/16]", "428 Precondition Required", "DELETE " + document.Documents, "HTTP 428 Precondition Required"},
		{"[10/16]", "412 Precondition Failed", "DELETE " + document.Documents, "HTTP 412 Precondition Failed"},
		{"[11/16]", "Names are unique", "POST " + document.Documents, "HTTP 409 Conflict"},
		{"[12/16]", "checks its scope first", "GET " + document.Documents + "/" + fakeFinanceID, "HTTP 404 Not Found"},
		{"[13/16]", "A small branch", "POST " + document.Documents, "HTTP 201 Created"},
		{"[14/16]", "two stages", "DELETE " + document.Documents, "HTTP 202 Accepted"},
		{"[15/16]", "until it answers 404", "GET " + document.Documents, "HTTP 404 Not Found"},
		{"[16/16]", "actually clears", "POST " + stateRoute, "HTTP 200 OK"},
	} {
		flat := strings.Join(strings.Fields(step(t, out, c.heading)), " ")
		note, request, status := strings.Index(flat, c.note), strings.Index(flat, c.request), strings.Index(flat, c.status)
		if note < 0 || request < 0 || status < 0 {
			t.Errorf("step %s lacks its note %q, request %q, or status %q:\n%s", c.heading, c.note, c.request, c.status, flat)
			continue
		}
		if note > request || request > status {
			t.Errorf("step %s is not note, request, response in order:\n%s", c.heading, flat)
		}
	}
}

func TestStorage_SummarizesTheLogoBytesAndShowsItsValidators(t *testing.T) {
	out, err := runStorage(t, newStorageFake(t))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	read := step(t, out, "[3/16]")
	for _, want := range []string{
		fmt.Sprintf("    (%d bytes of image/png)\n", len(fakeSeedLogo)),
		"    ETag: \"0x1\"\n",
		"    Last-Modified: ",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("the logo read lacks %q:\n%s", want, read)
		}
	}
	if strings.Contains(read, "PNG") {
		t.Errorf("the logo read prints the bytes:\n%s", read)
	}
	if replace := step(t, out, "[5/16]"); !strings.Contains(replace, "bytes of "+FixturesDir+"/"+replacementFixture+")") {
		t.Errorf("the replacement's request does not name its fixture:\n%s", replace)
	}
}

func TestStorage_PrintsTheTreeTheDeletingStatusAndTheSeededCounts(t *testing.T) {
	fake := newStorageFake(t)
	out, err := runStorage(t, fake)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	tree, _ := fake.seed.tree(storageOrgPath)
	paths := tree.paths()
	treeStep := step(t, out, "[7/16]")
	if !strings.Contains(treeStep, "  acme's document tree\n") {
		t.Errorf("the tree step lacks its table:\n%s", treeStep)
	}
	for p, id := range paths {
		if !strings.Contains(treeStep, p) || !strings.Contains(treeStep, id) {
			t.Errorf("the tree table lacks %s at %s", p, id)
		}
	}
	if !strings.Contains(step(t, out, "[14/16]"), `"status": "deleting"`) {
		t.Errorf("the recursive delete does not show the deleting status:\n%s", out)
	}
	if !strings.Contains(step(t, out, "[15/16]"), "· the sweep removed the branch within ") {
		t.Errorf("the sweep step does not report the wait:\n%s", out)
	}
	counts := fake.seed.seedCounts()
	reset := step(t, out, "[16/16]")
	for _, row := range []string{
		fmt.Sprintf("organizations  %d", counts.Organizations),
		fmt.Sprintf("logos          %d", counts.Logos),
		fmt.Sprintf("documents      %d", counts.Documents),
	} {
		if !strings.Contains(reset, row) {
			t.Errorf("the reset's table lacks %q:\n%s", row, reset)
		}
	}
}

func TestStorage_KeepsEveryNoteLineWithinEightyColumns(t *testing.T) {
	out, err := runStorage(t, newStorageFake(t))
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		// A note line is indented two spaces; a request or status line
		// shares the indent but carries a path, which may run past 80.
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") || len(line) <= 80 {
			continue
		}
		if first, _, _ := strings.Cut(strings.TrimSpace(line), " "); first == "HTTP" || strings.ToUpper(first) == first {
			continue
		}
		t.Errorf("note line is %d columns: %q", len(line), line)
	}
}

func TestSeedFile_CountsAndPathsFromDefault(t *testing.T) {
	seed, err := loadSeed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := seed.seedCounts(), (seeded{Documents: 10, Logos: 7, Organizations: 7}); got != want {
		t.Errorf("seedCounts = %+v, want %+v", got, want)
	}
	tree, err := seed.tree(storageOrgPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := tree.paths()
	for p, id := range map[string]string{
		guardedFilePath:                     "5eed0002-0000-4000-8000-000000000001",
		takenDirPath + "/":                  "5eed0002-0000-4000-8000-000000000002",
		"/engineering/platform/runbook.txt": "5eed0002-0000-4000-8000-000000000005",
	} {
		if paths[p] != id {
			t.Errorf("paths[%s] = %q, want %s", p, paths[p], id)
		}
	}
	if _, err := seed.tree("/nowhere"); err == nil {
		t.Error("tree(/nowhere) = nil error, want one naming the path")
	}
}

func TestInOrder(t *testing.T) {
	for _, c := range []struct {
		got, want []string
		ok        bool
	}{
		{[]string{"a", "b", "c"}, []string{"a", "c"}, true},
		{[]string{"a", "x", "b"}, []string{"a", "b"}, true},
		{[]string{"b", "a"}, []string{"a", "b"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
	} {
		if got := inOrder(c.got, c.want); got != c.ok {
			t.Errorf("inOrder(%v, %v) = %t, want %t", c.got, c.want, got, c.ok)
		}
	}
}

func TestSummarized(t *testing.T) {
	bin := &httpx.Response{Status: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: []byte{0x89, 'P', 'N', 'G'}}
	if got := string(summarized(bin).Body); got != "(4 bytes of image/png)" {
		t.Errorf("summarized binary body = %q", got)
	}
	if string(bin.Body) != "\x89PNG" {
		t.Error("summarized changed the response it was given")
	}
	js := &httpx.Response{Status: 404, Body: []byte(`{"status":404}`)}
	if summarized(js) != js {
		t.Error("summarized replaced a JSON body")
	}
}
