package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// codePattern is the layer's code rule, domain/organization's codePattern,
// repeated here so the test pins the created code to the rule it must pass.
var codePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestCreateBody_IsAValidOrganizationUnderItsParent(t *testing.T) {
	parent := "00000000-0000-0000-0000-000000000001"
	body := createOrganization{ParentID: &parent, Code: createdCode, Name: createdName}
	if !codePattern.MatchString(body.Code) {
		t.Errorf("code %q does not match %s", body.Code, codePattern)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":"` + parent + `","code":"sales","name":"Sales"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}

func TestRawQuery_EscapesValuesAndLeavesNamesAsWritten(t *testing.T) {
	got := rawQuery([2]string{"code[like]", "%o%"}, [2]string{"size", "2"}, [2]string{"page", "1"})
	if want := "code[like]=%25o%25&size=2&page=1"; got != want {
		t.Errorf("rawQuery = %q, want %q", got, want)
	}
}

func TestIfMatch_QuotesTheVersion(t *testing.T) {
	h := ifMatch(3)
	if h.Name != "If-Match" || h.Value != `"3"` {
		t.Errorf("ifMatch(3) = %+v, want If-Match: \"3\"", h)
	}
}

func TestIsTraceID(t *testing.T) {
	for id, want := range map[string]bool{
		"4bf92f3577b34da6a3ce929d0e0e4736":  true,
		"":                                  false,
		"4bf92f3577b34da6a3ce929d0e0e473":   false, // 31
		"4bf92f3577b34da6a3ce929d0e0e47366": false, // 33
		"4BF92F3577B34DA6A3CE929D0E0E4736":  false, // uppercase
		"4bf92f3577b34da6a3ce929d0e0e473g":  false, // not hex
		"req-4bf92f3577b34da6a3ce929d0e0e":  false, // a generated request id
	} {
		if got := isTraceID(id); got != want {
			t.Errorf("isTraceID(%q) = %v, want %v", id, got, want)
		}
	}
}

// fakeService stands in for the service, Tempo, and Grafana on one
// listener: the routes do not overlap. It holds the organization tree in
// memory, reseeds it on the admin reset, checks every command's If-Match
// against the row's version, and records each request's line and
// precondition so the test can check what the steps sent.
type fakeService struct {
	mu       sync.Mutex
	rows     []*organization
	trace    string
	requests []string
	err      error
}

const fakeTrace = "4bf92f3577b34da6a3ce929d0e0e4736"

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
	f := &fakeService{trace: fakeTrace}
	f.reseed()
	return f
}

func (f *fakeService) reseed() {
	f.rows = nil
	for i, row := range seededTree {
		o := &organization{ID: fakeID(i + 1), Code: row[1], Name: row[2], Version: 1}
		if row[0] != "" {
			id := f.byCode(row[0]).ID
			o.ParentID = &id
		}
		f.rows = append(f.rows, o)
	}
}

func fakeID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

func (f *fakeService) byCode(code string) *organization {
	for _, o := range f.rows {
		if o.Code == code {
			return o
		}
	}
	return nil
}

func (f *fakeService) byID(id string) *organization {
	for _, o := range f.rows {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (f *fakeService) pathOf(o *organization) string {
	if o.ParentID == nil {
		return "/" + o.Code
	}
	return f.pathOf(f.byID(*o.ParentID)) + "/" + o.Code
}

// view is o as the read model presents it, its path composed.
func (f *fakeService) view(o *organization) organization {
	v := *o
	v.Path = f.pathOf(o)
	return v
}

// fail records the first defect in what the scenario sent, for the test to
// report, and answers it with 400.
func (f *fakeService) fail(w http.ResponseWriter, format string, args ...any) {
	if f.err == nil {
		f.err = fmt.Errorf(format, args...)
	}
	w.WriteHeader(http.StatusBadRequest)
}

// command checks a command's If-Match against the row's version and
// returns the row, or nil after answering the mismatch.
func (f *fakeService) command(w http.ResponseWriter, r *http.Request, id string) *organization {
	o := f.byID(id)
	if o == nil {
		f.fail(w, "%s %s: no row %s", r.Method, r.URL.Path, id)
		return nil
	}
	if want := fmt.Sprintf(`"%d"`, o.Version); r.Header.Get("If-Match") != want {
		f.fail(w, "%s %s: If-Match %q, want %s", r.Method, r.URL.Path, r.Header.Get("If-Match"), want)
		return nil
	}
	return o
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

	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/api/health"):
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == "/api/traces/"+f.trace:
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Path == statePath:
		f.reseed()
		writeJSON(w, http.StatusOK, map[string]string{"state": seedState})
	case r.Method == http.MethodGet && r.URL.Path == organizationsPath:
		items := make([]organization, 0, len(f.rows))
		for _, o := range f.rows {
			items = append(items, f.view(o))
		}
		size := 20
		if r.URL.Query().Get("size") == "2" {
			size = 2
			items = items[:2]
		}
		writeJSON(w, http.StatusOK, page{Items: items, Page: 1, Size: size, Total: len(f.rows)})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, organizationsPath+"/path/"):
		for _, o := range f.rows {
			if "/"+strings.TrimPrefix(r.URL.Path, organizationsPath+"/path/") == f.pathOf(o) {
				writeJSON(w, http.StatusOK, f.view(o))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, organizationsPath+"/"):
		o := f.byID(strings.TrimPrefix(r.URL.Path, organizationsPath+"/"))
		if o == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, f.view(o))
	case r.Method == http.MethodPost && r.URL.Path == organizationsPath:
		var body createOrganization
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, "create: %v", err)
			return
		}
		o := &organization{ID: fakeID(len(f.rows) + 1), ParentID: body.ParentID, Code: body.Code, Name: body.Name, Version: 1}
		f.rows = append(f.rows, o)
		w.Header().Set("X-Request-Id", f.trace)
		w.Header().Set("Location", organizationsPath+"/"+o.ID)
		writeJSON(w, http.StatusCreated, identity{ID: o.ID, Version: o.Version})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transfer"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, organizationsPath+"/"), "/transfer")
		o := f.command(w, r, id)
		if o == nil {
			return
		}
		var body struct {
			ParentID *string `json:"parent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, "transfer: %v", err)
			return
		}
		o.ParentID = body.ParentID
		o.Version++
		writeJSON(w, http.StatusOK, identity{ID: o.ID, Version: o.Version})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, organizationsPath+"/"):
		o := f.command(w, r, strings.TrimPrefix(r.URL.Path, organizationsPath+"/"))
		if o == nil {
			return
		}
		var body editOrganization
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.fail(w, "edit: %v", err)
			return
		}
		o.Code, o.Name = body.Code, body.Name
		o.Version++
		writeJSON(w, http.StatusOK, identity{ID: o.ID, Version: o.Version})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, organizationsPath+"/"):
		o := f.command(w, r, strings.TrimPrefix(r.URL.Path, organizationsPath+"/"))
		if o == nil {
			return
		}
		f.rows = append(f.rows[:f.index(o)], f.rows[f.index(o)+1:]...)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, "unexpected request %s", line)
	}
}

func (f *fakeService) index(o *organization) int {
	for i, row := range f.rows {
		if row == o {
			return i
		}
	}
	return -1
}

// runDomain runs the domain scenario against a fake of the service, Tempo,
// and Grafana, from the working directory under the repository (so the
// reset step reads the real seed file), and returns the narration.
func runDomain(t *testing.T) (*fakeService, string) {
	t.Helper()
	s, ok := scenario.Lookup("domain")
	if !ok {
		t.Fatal("domain is not registered")
	}
	fake := newFakeService()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	ctx := scenario.WithEnv(context.Background(), scenario.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
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
	ctx := scenario.WithEnv(context.Background(), scenario.Env{Base: srv.URL, Grafana: srv.URL, Tempo: srv.URL})
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
	ctx := scenario.WithEnv(context.Background(), scenario.Env{Base: service.URL, Grafana: grafana.URL, Tempo: grafana.URL})
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
		"domain  The organization domain's full CRUD surface against the running service",
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
