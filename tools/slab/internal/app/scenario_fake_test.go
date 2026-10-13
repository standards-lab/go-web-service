package app_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/input"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// The fakes the domain, problems, and storage scenarios run against at the
// App seam, each on an httptest listener the run reaches through
// SLAB_BASE and SLAB_GRAFANA. orgFake stands in for the service's
// organization surface and Grafana on one listener; storageFake for its
// storage surface. Each answers the service's readiness probe, /readyz,
// and Grafana's health probe, /api/health, with 200, so the need nodes
// start.

// codePattern is the layer's code rule, domain/organization's codePattern,
// repeated here so the fake refuses a code the service would.
var codePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// orgFake stands in for the service and Grafana on one
// listener: the routes do not overlap. It holds the organization tree in
// memory, reseeds it on the admin reset, and records each request's line
// and precondition so the test can check what the steps sent.
//
// Each route makes the checks the real handler makes, in the real order,
// and a rejection is the problem document the real service would write:
// the SDK's own parsers reject a path id, a query, a body, and an If-Match
// header, and the fake's own vocabulary stands in for the domain's and the
// store's, mapped to statuses the way domain/organization's status and
// data's Status map them. Every response carries the trace id in
// X-Request-Id, as the service's request-id middleware does.
type orgFake struct {
	mu       sync.Mutex
	rows     []*organization.Organization
	trace    string
	problems *web.ErrorWriter
	requests []string
	err      error
}

const fakeTrace = "4bf92f3577b34da6a3ce929d0e0e4736"

// fakeMaxPageSize is the largest size a list request may ask for:
// internal/config/reads.go's defaultReadsMaxSize.
const fakeMaxPageSize = 100

// fakeMaxCommandBody is the byte limit the service puts on a command's
// request body: domain/organization/handler.go's maxCommandBody.
const fakeMaxCommandBody = 1 << 16

// stateRoute is the database admin service's reset, the route the
// scenarios' first step posts to.
const stateRoute = database.Database + "/state"

// The fake's error vocabulary: the domain's validation and cycle errors, the
// path parser's, and the store's stale version and constraint violation,
// with the wording the real ones carry. An absent row is sql.ErrNoRows, as
// the store returns it.
var (
	errPathID     = errors.New("must be a UUID")
	errValidation = errors.New("invalid command")
	errCycle      = errors.New("transfer would create a cycle")
	errStale      = errors.New("version mismatch")
	errConstraint = errors.New("constraint violation")
)

// fakeStatus maps the fake's vocabulary to statuses as the service's two
// matchers do. The SDK's own errors map themselves ahead of it.
func fakeStatus(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, errPathID), errors.Is(err, errValidation):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, errCycle), errors.Is(err, errConstraint):
		return web.Problem{Status: http.StatusConflict}, true
	case errors.Is(err, errStale):
		return web.Problem{Status: http.StatusPreconditionFailed}, true
	}
	return web.Problem{}, false
}

// seededTree is data/seeds/default.json as the fake holds it: parent code,
// code, name, in file order.
var seededTree = [][3]string{
	{"", "acme", "Acme Corporation"},
	{"acme", "engineering", "Engineering"},
	{"engineering", "platform", "Platform"},
	{"engineering", "product", "Product"},
	{"acme", "operations", "Operations"},
	{"operations", "logistics", "Logistics"},
	{"acme", "finance", "Finance"},
}

func newOrgFake() *orgFake {
	f := &orgFake{trace: fakeTrace, problems: web.NewErrorWriter(slog.New(slog.DiscardHandler), fakeStatus)}
	f.reseed()
	return f
}

func (f *orgFake) reseed() {
	f.rows = nil
	for i, row := range seededTree {
		o := &organization.Organization{ID: fakeID(i + 1), Code: row[1], Name: row[2], Version: 1}
		if row[0] != "" {
			id := f.byCode(row[0]).ID
			o.ParentID = &id
		}
		f.rows = append(f.rows, o)
	}
}

func fakeID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

func (f *orgFake) byCode(code string) *organization.Organization {
	for _, o := range f.rows {
		if o.Code == code {
			return o
		}
	}
	return nil
}

func (f *orgFake) byID(id string) *organization.Organization {
	for _, o := range f.rows {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (f *orgFake) pathOf(o *organization.Organization) string {
	if o.ParentID == nil {
		return "/" + o.Code
	}
	return f.pathOf(f.byID(*o.ParentID)) + "/" + o.Code
}

// view is o as the read model presents it, its path composed.
func (f *orgFake) view(o *organization.Organization) organization.Organization {
	v := *o
	v.Path = f.pathOf(o)
	return v
}

// fail records a request no route answers, for the test to report, and
// answers it with 400 and no body: a scenario's own mistake, not a problem
// the service would write.
func (f *orgFake) fail(w http.ResponseWriter, format string, args ...any) {
	if f.err == nil {
		f.err = fmt.Errorf(format, args...)
	}
	w.WriteHeader(http.StatusBadRequest)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *orgFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := r.Method + " " + r.URL.RequestURI()
	if m := r.Header.Get("If-Match"); m != "" {
		line += " If-Match: " + m
	}
	f.requests = append(f.requests, line)

	w.Header().Set("X-Request-Id", f.trace)
	r = r.WithContext(web.WithRequestID(r.Context(), f.trace))
	if err := f.route(w, r); err != nil {
		_ = f.problems.Write(w, r, err)
	}
}

// route dispatches r to the handler for its method and path, the way the
// service's router does. A handler's error is the problem the response
// reports.
func (f *orgFake) route(w http.ResponseWriter, r *http.Request) error {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/readyz" || r.URL.Path == "/api/health"):
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == stateRoute:
		f.reseed()
		writeJSON(w, http.StatusOK, map[string]string{"state": "default"})
	case r.Method == http.MethodGet && r.URL.Path == organization.Organizations:
		return f.list(w, r)
	case r.Method == http.MethodGet && r.URL.Path == organization.Organizations+"/lookup":
		for _, o := range f.rows {
			if r.URL.Query().Get("path") == f.pathOf(o) {
				writeJSON(w, http.StatusOK, f.view(o))
				return nil
			}
		}
		return sql.ErrNoRows
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, organization.Organizations+"/"):
		return f.find(w, r)
	case r.Method == http.MethodPost && r.URL.Path == organization.Organizations:
		return f.create(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transfer"):
		return f.transfer(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, organization.Organizations+"/"):
		return f.edit(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, organization.Organizations+"/"):
		return f.delete(w, r)
	default:
		f.fail(w, "unexpected request %s %s", r.Method, r.URL.RequestURI())
	}
	return nil
}

// pathID reads the {id} segment of r's path and parses it as a UUID, the
// check web.PathUUID makes, with its wording.
func pathID(r *http.Request) (string, error) {
	raw := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, organization.Organizations+"/"), "/transfer")
	if _, err := uuid.Parse(raw); err != nil {
		return "", fmt.Errorf("path id=%q: %w", raw, errPathID)
	}
	return raw, nil
}

// command reads a guarded command's three inputs in the order sdk.Command
// does: the path id, the If-Match version, and the body, strictly decoded
// and bounded at the command body limit.
func orgCommand[T any](w http.ResponseWriter, r *http.Request) (id string, version int64, body T, err error) {
	if id, err = pathID(r); err != nil {
		return "", 0, body, err
	}
	if version, err = web.IfMatch(r); err != nil {
		return "", 0, body, err
	}
	if body, err = web.DecodeJSON[T](w, r, fakeMaxCommandBody); err != nil {
		return "", 0, body, err
	}
	return id, version, body, nil
}

// validate is the create and edit commands' own rules, with the wording
// domain/organization's validCode and validName carry.
func orgValidate(code, name string) error {
	if !codePattern.MatchString(code) {
		return fmt.Errorf("%w: code must be lowercase words joined by single hyphens", errValidation)
	}
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", errValidation)
	}
	return nil
}

// guard is the store's version guard: the row at id, at version, or the
// error the guard returns when there is no such row or it is at another
// version.
func (f *orgFake) guard(id string, version int64) (*organization.Organization, error) {
	o := f.byID(id)
	if o == nil {
		return nil, sql.ErrNoRows
	}
	if o.Version != version {
		return nil, fmt.Errorf("%w: expected %d, current %d", errStale, version, o.Version)
	}
	return o, nil
}

// list parses the query as the handler does and pages the rows by it. The
// filters are not applied: no test reads the queried list's items.
func (f *orgFake) list(w http.ResponseWriter, r *http.Request) error {
	q, err := web.ParseQuery(r.URL.Query(), web.Limits{DefaultSize: input.DefaultPageSize, MaxSize: fakeMaxPageSize})
	if err != nil {
		return err
	}
	items := make([]organization.Organization, 0, len(f.rows))
	for _, o := range f.rows {
		items = append(items, f.view(o))
	}
	start := min((q.Page-1)*q.Size, len(items))
	end := min(start+q.Size, len(items))
	writeJSON(w, http.StatusOK, web.NewPage(items[start:end], q, web.Paging{Total: new(len(f.rows)), More: end < len(items)}))
	return nil
}

func (f *orgFake) find(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	o := f.byID(id)
	if o == nil {
		return sql.ErrNoRows
	}
	writeJSON(w, http.StatusOK, f.view(o))
	return nil
}

// create decodes and validates the body, then makes the store's two checks
// as the schema's constraints would: the parent must exist (the foreign
// key) and no sibling may carry the code (the unique constraint, null
// parents equal).
func (f *orgFake) create(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[organization.CreateOrganization](w, r, fakeMaxCommandBody)
	if err != nil {
		return err
	}
	if err := orgValidate(body.Code, body.Name); err != nil {
		return err
	}
	if body.ParentID != nil && f.byID(*body.ParentID) == nil {
		return fmt.Errorf("%w: parent %s does not exist", errConstraint, *body.ParentID)
	}
	for _, o := range f.rows {
		if o.Code == body.Code && sameParent(o.ParentID, body.ParentID) {
			return fmt.Errorf("%w: code %q exists under the same parent", errConstraint, body.Code)
		}
	}
	o := &organization.Organization{ID: fakeID(len(f.rows) + 1), ParentID: body.ParentID, Code: body.Code, Name: body.Name, Version: 1}
	f.rows = append(f.rows, o)
	w.Header().Set("Location", organization.Organizations+"/"+o.ID)
	writeJSON(w, http.StatusCreated, organization.Identity{ID: o.ID, Version: o.Version})
	return nil
}

func sameParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (f *orgFake) edit(w http.ResponseWriter, r *http.Request) error {
	id, version, body, err := orgCommand[organization.EditOrganization](w, r)
	if err != nil {
		return err
	}
	if err := orgValidate(body.Code, body.Name); err != nil {
		return err
	}
	o, err := f.guard(id, version)
	if err != nil {
		return err
	}
	o.Code, o.Name = body.Code, body.Name
	o.Version++
	writeJSON(w, http.StatusOK, organization.Identity{ID: o.ID, Version: o.Version})
	return nil
}

// transfer checks the destination against the subtree before the guard,
// as the store does under the tree lock.
func (f *orgFake) transfer(w http.ResponseWriter, r *http.Request) error {
	type transferOrganization struct {
		ParentID *string `json:"parent_id"`
	}
	id, version, body, err := orgCommand[transferOrganization](w, r)
	if err != nil {
		return err
	}
	if body.ParentID != nil {
		for p := f.byID(*body.ParentID); p != nil; p = f.parentOf(p) {
			if p.ID == id {
				return fmt.Errorf("%w: %s is in the subtree of %s", errCycle, *body.ParentID, id)
			}
		}
	}
	o, err := f.guard(id, version)
	if err != nil {
		return err
	}
	o.ParentID = body.ParentID
	o.Version++
	writeJSON(w, http.StatusOK, organization.Identity{ID: o.ID, Version: o.Version})
	return nil
}

func (f *orgFake) parentOf(o *organization.Organization) *organization.Organization {
	if o.ParentID == nil {
		return nil
	}
	return f.byID(*o.ParentID)
}

func (f *orgFake) delete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	version, err := web.IfMatch(r)
	if err != nil {
		return err
	}
	o, err := f.guard(id, version)
	if err != nil {
		return err
	}
	for _, child := range f.rows {
		if child.ParentID != nil && *child.ParentID == id {
			return fmt.Errorf("%w: %s has children", errConstraint, id)
		}
	}
	f.rows = append(f.rows[:f.index(o)], f.rows[f.index(o)+1:]...)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (f *orgFake) index(o *organization.Organization) int {
	for i, row := range f.rows {
		if row == o {
			return i
		}
	}
	return -1
}

// The storage scenario's fixed targets as the service states them: acme,
// whose logo and tree it walks, finance, the other organization its
// isolation step names, the seeded file and directory its refusals target,
// the fixture its logo replacement uploads, and the admin routes it reads.
const (
	acmePath           = "/acme"
	financePath        = "/acme/finance"
	guardedFilePath    = "/README.txt"
	takenDirPath       = "/engineering"
	replacementFixture = "engineering.png"
	schemaRoute        = database.Database + "/schema"
	seedRoute          = database.Database + "/seed"
)

// detailNameTaken is the curated detail of a taken name's 409, the
// service's data.DetailNameTaken.
const detailNameTaken = "an entry with that name already exists"

// seedFile is data/seeds/default.json as the fake reads it, decoded here
// independently of the scenario's own reading so the counts and ids the
// scenario checks are not the scenario's own arithmetic.
type seedFile struct {
	Organizations []json.RawMessage `json:"organizations"`
	Logos         []json.RawMessage `json:"logos"`
	Documents     []seedTree        `json:"documents"`
}

type seedTree struct {
	Organization string      `json:"organization"`
	Root         string      `json:"root"`
	Entries      []seedEntry `json:"entries"`
}

type seedEntry struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ContentType string      `json:"content_type"`
	Entries     []seedEntry `json:"entries"`
}

// seeded holds the counts the service's seeder reports.
type seeded struct {
	Documents     int `json:"documents"`
	Logos         int `json:"logos"`
	Organizations int `json:"organizations"`
}

// loadSeed reads the repository's default seed file.
func loadSeed(t *testing.T) seedFile {
	t.Helper()
	fsys, err := repo.FS("")
	if err != nil {
		t.Fatalf("find the repository: %v", err)
	}
	raw, err := fs.ReadFile(fsys, "data/seeds/default.json")
	if err != nil {
		t.Fatalf("read the seed: %v", err)
	}
	var s seedFile
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode the seed: %v", err)
	}
	return s
}

// counts is what the service reports seeding for s: every organization,
// every logo, and every document entry below the roots.
func (s seedFile) counts() seeded {
	n := 0
	var count func([]seedEntry)
	count = func(entries []seedEntry) {
		for _, e := range entries {
			n++
			count(e.Entries)
		}
	}
	for _, t := range s.Documents {
		count(t.Entries)
	}
	return seeded{Documents: n, Logos: len(s.Logos), Organizations: len(s.Organizations)}
}

// acme is acme's seeded tree.
func (s seedFile) acme(t *testing.T) seedTree {
	t.Helper()
	for _, tree := range s.Documents {
		if tree.Organization == acmePath {
			return tree
		}
	}
	t.Fatalf("the seed has no document tree for %s", acmePath)
	return seedTree{}
}

// paths is every entry of t by its path under the root, a directory's with
// a trailing slash, mapped to its fixed id.
func (t seedTree) paths() map[string]string {
	out := make(map[string]string)
	var walk func(prefix string, entries []seedEntry)
	walk = func(prefix string, entries []seedEntry) {
		for _, e := range entries {
			p := prefix + e.Name
			if e.ContentType == "" {
				out[p+"/"] = e.ID
				walk(p+"/", e.Entries)
				continue
			}
			out[p] = e.ID
		}
	}
	walk("/", t.Entries)
	return out
}

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
// one request line ("METHOD path") the fake answers 500 instead,
// takenDetail replaces a taken name's curated detail, miscount adds to the
// documents a reset reports seeding, and descendingWalk serves a paged
// directory listing in descending name order whatever its sort says, for
// the unexpected-response tests.
type storageFake struct {
	mu             sync.Mutex
	seed           seedFile
	acmeTree       seedTree
	logo           []byte
	logoTag        int
	nodes          map[string]*fakeNode
	deleting       map[string]int
	serial         int
	requests       []string
	fault          string
	takenDetail    string
	miscount       int
	descendingWalk bool
	err            error
}

// resetCounts is what a reset reports seeding: the seed file's counts, the
// documents off by miscount.
func (f *storageFake) resetCounts() seeded {
	c := f.seed.counts()
	c.Documents += f.miscount
	return c
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
	f := &storageFake{seed: loadSeed(t), takenDetail: detailNameTaken}
	f.acmeTree = f.seed.acme(t)
	f.reseed()
	return f
}

// reseed restores the seeded logo and acme's seeded tree, as a reset does.
func (f *storageFake) reseed() {
	f.logo, f.logoTag = fakeSeedLogo, f.logoTag+1
	f.nodes, f.deleting = map[string]*fakeNode{}, map[string]int{}
	tree := f.acmeTree
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
	case r.Method == http.MethodGet && p == "/readyz":
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
		writeJSON(w, http.StatusOK, map[string]any{"state": "default", "schema": fakeSchema, "seeded": f.resetCounts()})
	case r.Method == http.MethodGet && p == organization.Organizations+"/lookup":
		switch r.URL.Query().Get("path") {
		case acmePath:
			writeJSON(w, http.StatusOK, organization.Organization{ID: fakeAcmeID, Code: "acme", Version: 1, Path: acmePath})
		case financePath:
			writeJSON(w, http.StatusOK, organization.Organization{ID: fakeFinanceID, Code: "finance", Version: 1, Path: financePath})
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
func storageIfMatch(w http.ResponseWriter, r *http.Request, want int64) bool {
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
			if storageIfMatch(w, r, n.version) {
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
		if !storageIfMatch(w, r, n.version) {
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
	case len(rest) == 1 && rest[0] == "files" && r.Method == http.MethodPost && r.URL.Query().Get("name") != "":
		if f.inDeleting(n) {
			writeProblem(w, http.StatusConflict, "the directory is being deleted")
			return
		}
		file := &fakeNode{id: f.newID(), parent: n.id, name: r.URL.Query().Get("name"), contentType: r.Header.Get("Content-Type"), status: "available", version: 2}
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
	size := input.DefaultPageSize
	if s := q.Get("size"); s != "" {
		size, _ = strconv.Atoi(s)
	}
	all := f.children(n.id, dirs)
	// past reports whether name follows after in the order served.
	past := func(name, after string) bool { return name > after }
	if f.descendingWalk && dirs && q.Get("size") != "" {
		slices.Reverse(all)
		past = func(name, after string) bool { return name < after }
	}
	total := len(all)
	rows, page := all, 1
	if c := q.Get("cursor"); c != "" {
		after := strings.TrimPrefix(c, "after:")
		rows, page = nil, 0
		for _, n := range all {
			if past(n.name, after) {
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
