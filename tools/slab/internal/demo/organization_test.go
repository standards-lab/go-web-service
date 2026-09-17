package demo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/internal/api"
	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// codePattern is the layer's code rule, domain/organization's codePattern,
// repeated here so the test pins the created code to the rule it must pass.
var codePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestCreatedCode_IsAValidOrganizationCode(t *testing.T) {
	if !codePattern.MatchString(createdCode) {
		t.Errorf("code %q does not match %s", createdCode, codePattern)
	}
}

// fakeService stands in for the service, Tempo, and Grafana on one
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
type fakeService struct {
	mu       sync.Mutex
	rows     []*api.Organization
	trace    string
	problems *web.ErrorWriter
	requests []string
	err      error
}

const fakeTrace = "4bf92f3577b34da6a3ce929d0e0e4736"

// fakeMaxPageSize is the largest size a list request may ask for:
// internal/config/reads.go's defaultReadsMaxSize.
const fakeMaxPageSize = 100

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

func newFakeService() *fakeService {
	f := &fakeService{trace: fakeTrace, problems: web.NewErrorWriter(fakeStatus)}
	f.reseed()
	return f
}

func (f *fakeService) reseed() {
	f.rows = nil
	for i, row := range seededTree {
		o := &api.Organization{ID: fakeID(i + 1), Code: row[1], Name: row[2], Version: 1}
		if row[0] != "" {
			id := f.byCode(row[0]).ID
			o.ParentID = &id
		}
		f.rows = append(f.rows, o)
	}
}

func fakeID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

func (f *fakeService) byCode(code string) *api.Organization {
	for _, o := range f.rows {
		if o.Code == code {
			return o
		}
	}
	return nil
}

func (f *fakeService) byID(id string) *api.Organization {
	for _, o := range f.rows {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (f *fakeService) pathOf(o *api.Organization) string {
	if o.ParentID == nil {
		return "/" + o.Code
	}
	return f.pathOf(f.byID(*o.ParentID)) + "/" + o.Code
}

// view is o as the read model presents it, its path composed.
func (f *fakeService) view(o *api.Organization) api.Organization {
	v := *o
	v.Path = f.pathOf(o)
	return v
}

// fail records a request no route answers, for the test to report, and
// answers it with 400 and no body: a scenario's own mistake, not a problem
// the service would write.
func (f *fakeService) fail(w http.ResponseWriter, format string, args ...any) {
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

func (f *fakeService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (f *fakeService) route(w http.ResponseWriter, r *http.Request) error {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/api/health"):
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/api/traces/"+f.trace:
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == api.State:
		f.reseed()
		writeJSON(w, http.StatusOK, map[string]string{"state": api.SeedState})
	case r.Method == http.MethodGet && r.URL.Path == api.Organizations:
		return f.list(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, api.Organizations+"/path/"):
		for _, o := range f.rows {
			if "/"+strings.TrimPrefix(r.URL.Path, api.Organizations+"/path/") == f.pathOf(o) {
				writeJSON(w, http.StatusOK, f.view(o))
				return nil
			}
		}
		return sql.ErrNoRows
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, api.Organizations+"/"):
		return f.find(w, r)
	case r.Method == http.MethodPost && r.URL.Path == api.Organizations:
		return f.create(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transfer"):
		return f.transfer(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, api.Organizations+"/"):
		return f.edit(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, api.Organizations+"/"):
		return f.delete(w, r)
	default:
		f.fail(w, "unexpected request %s %s", r.Method, r.URL.RequestURI())
	}
	return nil
}

// pathID reads the {id} segment of r's path and parses it as a UUID, the
// check sdk.PathID makes, with its wording.
func pathID(r *http.Request) (string, error) {
	raw := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, api.Organizations+"/"), "/transfer")
	if _, err := uuid.Parse(raw); err != nil {
		return "", fmt.Errorf("path id=%q: %w", raw, errPathID)
	}
	return raw, nil
}

// command reads a guarded command's three inputs in the order sdk.Command
// does: the path id, the If-Match version, and the body, strictly decoded
// and bounded at the command body limit.
func command[T any](w http.ResponseWriter, r *http.Request) (id string, version int64, body T, err error) {
	if id, err = pathID(r); err != nil {
		return "", 0, body, err
	}
	if version, err = web.IfMatch(r); err != nil {
		return "", 0, body, err
	}
	if body, err = web.DecodeJSON[T](w, r, api.MaxCommandBody); err != nil {
		return "", 0, body, err
	}
	return id, version, body, nil
}

// validate is the create and edit commands' own rules, with the wording
// domain/organization's validCode and validName carry.
func validate(code, name string) error {
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
func (f *fakeService) guard(id string, version int64) (*api.Organization, error) {
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
func (f *fakeService) list(w http.ResponseWriter, r *http.Request) error {
	q, err := web.ParseQuery(r.URL.Query(), web.Limits{DefaultSize: api.DefaultPageSize, MaxSize: fakeMaxPageSize})
	if err != nil {
		return err
	}
	items := make([]api.Organization, 0, len(f.rows))
	for _, o := range f.rows {
		items = append(items, f.view(o))
	}
	start := min((q.Page-1)*q.Size, len(items))
	end := min(start+q.Size, len(items))
	writeJSON(w, http.StatusOK, api.Page{Items: items[start:end], Page: q.Page, Size: q.Size, Total: len(f.rows)})
	return nil
}

func (f *fakeService) find(w http.ResponseWriter, r *http.Request) error {
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
func (f *fakeService) create(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[api.CreateOrganization](w, r, api.MaxCommandBody)
	if err != nil {
		return err
	}
	if err := validate(body.Code, body.Name); err != nil {
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
	o := &api.Organization{ID: fakeID(len(f.rows) + 1), ParentID: body.ParentID, Code: body.Code, Name: body.Name, Version: 1}
	f.rows = append(f.rows, o)
	w.Header().Set("Location", api.Organizations+"/"+o.ID)
	writeJSON(w, http.StatusCreated, api.Identity{ID: o.ID, Version: o.Version})
	return nil
}

func sameParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (f *fakeService) edit(w http.ResponseWriter, r *http.Request) error {
	id, version, body, err := command[api.EditOrganization](w, r)
	if err != nil {
		return err
	}
	if err := validate(body.Code, body.Name); err != nil {
		return err
	}
	o, err := f.guard(id, version)
	if err != nil {
		return err
	}
	o.Code, o.Name = body.Code, body.Name
	o.Version++
	writeJSON(w, http.StatusOK, api.Identity{ID: o.ID, Version: o.Version})
	return nil
}

// transfer checks the destination against the subtree before the guard,
// as the store does under the tree lock.
func (f *fakeService) transfer(w http.ResponseWriter, r *http.Request) error {
	type transferOrganization struct {
		ParentID *string `json:"parent_id"`
	}
	id, version, body, err := command[transferOrganization](w, r)
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
	writeJSON(w, http.StatusOK, api.Identity{ID: o.ID, Version: o.Version})
	return nil
}

func (f *fakeService) parentOf(o *api.Organization) *api.Organization {
	if o.ParentID == nil {
		return nil
	}
	return f.byID(*o.ParentID)
}

func (f *fakeService) delete(w http.ResponseWriter, r *http.Request) error {
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

func (f *fakeService) index(o *api.Organization) int {
	for i, row := range f.rows {
		if row == o {
			return i
		}
	}
	return -1
}

// runDomain runs the domain scenario against the fake.
func runDomain(t *testing.T) (*fakeService, string) {
	t.Helper()
	return runScenario(t, "domain")
}

// runScenario runs the named scenario against a fake of the service, Tempo,
// and Grafana, from the working directory under the repository (so the
// reset step reads the real seed file), and returns the narration.
func runScenario(t *testing.T, name string) (*fakeService, string) {
	t.Helper()
	s, ok := scenario.Lookup(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	fake := newFakeService()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	ctx := env.WithContext(context.Background(), env.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
	var out bytes.Buffer
	err := scenario.Run(ctx, s, scenario.NewReporter(&out, false))
	// Close waits for every handler to return, so the fake's state is read
	// without its lock from here on.
	srv.Close()
	if fake.err != nil {
		t.Fatalf("the scenario sent a bad request: %v\n%s", fake.err, out.String())
	}
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return fake, out.String()
}

func TestScenario_RunsTenStepsInOrderAgainstTheFake(t *testing.T) {
	_, out := runDomain(t)
	steps := []string{
		"[1/10] Initialization", "[2/10] Raw List", "[3/10] Queried List", "[4/10] Find",
		"[5/10] Find by Path", "[6/10] Create", "[7/10] Edit", "[8/10] Transfer",
		"[9/10] Results", "[10/10] Delete",
	}
	last := -1
	for _, step := range steps {
		at := strings.Index(out, step)
		if at < 0 {
			t.Fatalf("output lacks %q:\n%s", step, out)
		}
		if at < last {
			t.Errorf("%q is out of order", step)
		}
		last = at
	}
	if strings.Contains(out, "[11/") {
		t.Errorf("the scenario narrates more than ten steps:\n%s", out)
	}
	if strings.Contains(out, "·") {
		t.Errorf("the trace poll ticks into the narration:\n%s", out)
	}
}

// step returns the narration of one step: from its heading to the next.
func step(t *testing.T, out, heading string) string {
	t.Helper()
	start := strings.Index(out, heading)
	if start < 0 {
		t.Fatalf("output lacks %q:\n%s", heading, out)
	}
	rest := out[start+len(heading):]
	if end := strings.Index(rest, "\n["); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestScenario_NarratesEachStepBeforeItsRequest(t *testing.T) {
	_, out := runDomain(t)
	for _, c := range []struct{ heading, note, request string }{
		{"[1/10]", `posts the "default" state`, "POST /admin/database/state"},
		{"[2/10]", "A list request with no query component", "GET /api/organizations\n"},
		{"[3/10]", "A filter is a query parameter named for a field", "GET /api/organizations?"},
		{"[6/10]", "This creates sales under acme", "POST /api/organizations\n"},
		{"[7/10]", "Rename the seeded finance row", "PUT /api/organizations/"},
		{"[8/10]", "A transfer is a structural move", "/transfer"},
		{"[9/10]", "Retrieving the raw list data again", "GET /api/organizations\n"},
		{"[10/10]", "Delete only the sales row", "DELETE /api/organizations/"},
	} {
		text := step(t, out, c.heading)
		note, request := strings.Index(text, c.note), strings.Index(text, c.request)
		if note < 0 || request < 0 {
			t.Errorf("step %s lacks %q or %q:\n%s", c.heading, c.note, c.request, text)
			continue
		}
		if note > request {
			t.Errorf("step %s: the note %q follows the request %q:\n%s", c.heading, c.note, c.request, text)
		}
	}
	// The create step's trace pointer follows its response, once known.
	create := step(t, out, "[6/10]")
	response, trace := strings.Index(create, "HTTP 201 Created"), strings.Index(create, "Trace ID    : "+fakeTrace)
	if response < 0 || trace < response {
		t.Errorf("the trace block does not follow the create response:\n%s", create)
	}
}

func TestScenario_ShowsTheSeedFileBeforeTheReset(t *testing.T) {
	_, out := runDomain(t)
	caption, reset := strings.Index(out, "  data/seeds/default.json\n"), strings.Index(out, "POST /admin/database/state")
	if caption < 0 || reset < 0 || caption > reset {
		t.Fatalf("the seed file does not precede the reset request:\n%s", out)
	}
	for _, want := range []string{`"organizations": [`, `"code": "acme"`, `"name": "Acme Corporation"`, `"parent": "operations"`} {
		if !strings.Contains(out[caption:reset], want) {
			t.Errorf("the seed block lacks %s:\n%s", want, out[caption:reset])
		}
	}
}

func TestScenario_SendsTheQueryWithLiteralBrackets(t *testing.T) {
	fake, out := runDomain(t)
	const line = "GET /api/organizations?code[like]=%25o%25&size=2&page=1"
	if !strings.Contains(out, "  "+line+"\n") {
		t.Errorf("output lacks the request line %q:\n%s", line, out)
	}
	if !contains(fake.requests, line) {
		t.Errorf("the fake did not receive %q; got:\n%s", line, strings.Join(fake.requests, "\n"))
	}
	if strings.Contains(out, "%5B") || strings.Contains(out, "%5D") {
		t.Errorf("the brackets are percent-encoded:\n%s", out)
	}
}

func TestScenario_TargetsThreeDifferentRowsEachAtItsOwnVersion(t *testing.T) {
	fake, out := runDomain(t)
	finance, logistics, engineering := fakeID(7), fakeID(6), fakeID(2)
	sales := fakeID(8)
	for _, want := range []string{
		"POST /admin/database/state",
		"GET /api/organizations",
		"GET /api/organizations/" + fakeID(1),
		"GET /api/organizations/path/acme/engineering/platform",
		"POST /api/organizations",
		"PUT /api/organizations/" + finance + ` If-Match: "1"`,
		"POST /api/organizations/" + logistics + `/transfer If-Match: "1"`,
		"GET /api/organizations",
		"DELETE /api/organizations/" + sales + ` If-Match: "1"`,
	} {
		if !contains(fake.requests, want) {
			t.Errorf("the fake did not receive %q; got:\n%s", want, strings.Join(fake.requests, "\n"))
		}
	}
	for _, want := range []string{
		`"code": "finance"`, `"name": "Finance and Accounting"`,
		`"parent_id": "` + engineering + `"`,
		`"code": "sales"`, `"name": "Sales"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	if o := fake.byCode("finance"); o.Name != "Finance and Accounting" || o.Version != 2 {
		t.Errorf("finance = %+v, want renamed at version 2", o)
	}
	if o := fake.byCode("logistics"); o.ParentID == nil || *o.ParentID != engineering || o.Version != 2 {
		t.Errorf("logistics = %+v, want under engineering at version 2", o)
	}
	if fake.byCode("sales") != nil {
		t.Error("sales survived the delete")
	}
	if got := len(fake.rows); got != 7 {
		t.Errorf("%d rows remain, want the seeded 7", got)
	}
}

func TestScenario_KeepsEveryNoteLineWithinEightyColumns(t *testing.T) {
	_, out := runDomain(t)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && len(line) > 80 {
			t.Errorf("line is %d columns: %q", len(line), line)
		}
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestScenario_StopsAtTheServiceNeedWhenNothingListens(t *testing.T) {
	s, ok := scenario.Lookup("domain")
	if !ok {
		t.Fatal("domain is not registered")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	ctx := env.WithContext(context.Background(), env.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
	var out bytes.Buffer
	err := scenario.Run(ctx, s, scenario.NewReporter(&out, false))
	if err == nil {
		t.Fatalf("run succeeded with nothing listening:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), `need "the service"`) {
		t.Errorf("error %q does not name the service need", err)
	}
	for _, want := range []string{"the service is not reachable", "start it with: mise run serve"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "[1/10]") {
		t.Errorf("run reached a step:\n%s", out.String())
	}
}

func TestScenario_StopsAtTheGrafanaNeedWhenOnlyTheServiceAnswers(t *testing.T) {
	s, _ := scenario.Lookup("domain")
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(service.Close)
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(grafana.Close)
	ctx := env.WithContext(context.Background(), env.Env{Base: service.URL, Grafana: grafana.URL, Tempo: grafana.URL})
	var out bytes.Buffer
	err := scenario.Run(ctx, s, scenario.NewReporter(&out, false))
	if err == nil || !strings.Contains(err.Error(), `need "Grafana"`) {
		t.Fatalf("run = %v, want the Grafana need to fail:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "start it with: mise run otel-up") {
		t.Errorf("output does not name the task:\n%s", out.String())
	}
}

func TestList_ShowsTheScenarioWithItsFourNeeds(t *testing.T) {
	var out bytes.Buffer
	root := cli.Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"list"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{
		"domain    The organization domain's full CRUD surface against the running service",
		"needs postgres (mise run db-up)",
		"needs the observability profile (mise run otel-up)",
		"needs the service (mise run serve)",
		"needs Grafana (mise run otel-up)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
}
