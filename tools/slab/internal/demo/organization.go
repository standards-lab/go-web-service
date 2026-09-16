package demo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/internal/repo"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// The routes the scenario calls. organizationsPath is domain/organization's
// group, /organizations, inside internal/app's /api mount; statePath is the
// database admin service's reset, inside the /admin mount.
const (
	organizationsPath = "/api/organizations"
	statePath         = "/admin/database/state"
)

// seedState is the named state the first step resets to, and seedsDir is
// where the service's states live, relative to the repository root: the
// first step shows data/seeds/default.json, the seven-row acme tree.
const (
	seedState = "default"
	seedsDir  = "data/seeds"
)

// defaultPageSize is the page size a list request gets when it names none:
// internal/config/reads.go's defaultReadsDefaultSize, which config.json
// leaves in force. The raw list step says so and checks the response
// agrees.
const defaultPageSize = 20

// The row the create step adds under acme and the delete step removes. The
// reset step guarantees it is absent when the run starts, so the code is a
// plain business unit rather than a run-stamped placeholder.
const (
	createdCode = "sales"
	createdName = "Sales"
)

// The seeded rows the edit and transfer steps change: finance is renamed,
// logistics moves from operations to engineering.
const (
	editedCode    = "finance"
	editedName    = "Finance and Accounting"
	movedCode     = "logistics"
	newParentCode = "engineering"
)

// serviceName is the service.name resource attribute the service exports,
// which Tempo and Loki index the trace under. slab does not depend on the
// service's module, so the value is repeated here from
// internal/app/telemetry.go's serviceName constant.
const serviceName = "go-web-service"

// traceIDPattern is the shape of a W3C trace id as the service renders it:
// 16 bytes, hex-encoded, lowercase.
var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func init() {
	scenario.Add(organizationScenario())
}

// The command bodies, the shapes of domain/organization's CreateOrganization
// and EditOrganization. slab does not depend on the service's module, so the
// fields are repeated here with the same json tags.
type createOrganization struct {
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
}

type editOrganization struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// identity is every command's success envelope, the shape of
// domain/organization's Identity.
type identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// organization is the read model as the API presents it, and page is the
// list's envelope around it.
type organization struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Version  int64   `json:"version"`
	Path     string  `json:"path"`
}

type page struct {
	Items []organization `json:"items"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
	Total int            `json:"total"`
}

// orgState is what the steps hand forward: the client the first step binds,
// the seeded rows by code from the first raw list, and the identity the
// create step's response returns. Each command targets a different row,
// and no row is written twice, so every If-Match reads the version from
// where the row was last seen: the seeded rows at the version the list
// showed, the created row at the version create returned.
type orgState struct {
	client  *httpx.Client
	byCode  map[string]organization
	created identity
}

func organizationScenario() scenario.Scenario {
	s := &orgState{}
	return scenario.Scenario{
		Name:    "domain",
		Summary: "The organization domain's full CRUD surface against the running service, reseeded from a known fixture each run, with the create request's trace located in Grafana (needs the full compose stack)",
		Needs: []scenario.Need{
			{What: "postgres", Task: "db-up"},
			{What: "the observability profile", Task: "otel-up"},
			{What: "the service", Task: "serve", Check: func(ctx context.Context) error {
				return httpx.Live(ctx, scenario.EnvFrom(ctx).Base)
			}},
			{What: "Grafana", Task: "otel-up", Check: grafanaHealthy},
		},
		Steps: []scenario.Step{
			{Intent: "Initialization", Action: s.reset},
			{Intent: "Raw List", Action: s.list},
			{Intent: "Queried List", Action: s.query},
			{Intent: "Find", Action: s.find},
			{Intent: "Find by Path", Action: s.findByPath},
			{Intent: "Create", Action: s.create},
			{Intent: "Edit", Action: s.edit},
			{Intent: "Transfer", Action: s.transfer},
			{Intent: "Results", Action: s.listAgain},
			{Intent: "Delete", Action: s.delete},
		},
	}
}

// grafanaHealthy is the Need check for Grafana: its health endpoint answers
// 200. It is one request, so it builds its own client and lets it go.
func grafanaHealthy(ctx context.Context) error {
	res, err := httpx.NewClient(scenario.EnvFrom(ctx).Grafana).Get(ctx, "/api/health")
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return fmt.Errorf("GET /api/health: %w", err)
	}
	return nil
}

// isTraceID reports whether s has the shape of a trace id.
func isTraceID(s string) bool {
	return traceIDPattern.MatchString(s)
}

// ifMatch is the precondition header a command carries: the version the
// caller last saw, quoted as an entity tag.
func ifMatch(version int64) httpx.Header {
	return httpx.Header{Name: "If-Match", Value: fmt.Sprintf(`"%d"`, version)}
}

// rawQuery joins name=value pairs into a query string with each value
// percent-encoded and each name left as written. url.Values.Encode would
// escape the brackets of a name like code[like] to %5B and %5D, and the
// server reads them either way; the literal form is what the printed
// request should show.
func rawQuery(pairs ...[2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p[0] + "=" + url.QueryEscape(p[1])
	}
	return strings.Join(parts, "&")
}

// seeded returns the seeded row with code, from the raw list step.
func (s *orgState) seeded(code string) (organization, error) {
	o, ok := s.byCode[code]
	if !ok {
		return organization{}, fmt.Errorf("the seeded tree has no organization with code %q", code)
	}
	return o, nil
}

// identityOf reads the Identity envelope off a command's response and checks
// it names the row the command targeted.
func identityOf(res *httpx.Response, wantID string) (identity, error) {
	var id identity
	if err := res.JSON(&id); err != nil {
		return identity{}, err
	}
	if id.ID != wantID {
		return identity{}, fmt.Errorf("the response names id %s, want %s", id.ID, wantID)
	}
	return id, nil
}

// expectVersion checks a command left its row at version want: one past
// the version its If-Match carried.
func expectVersion(id identity, want int64) error {
	if id.Version != want {
		return fmt.Errorf("the row is at version %d, want %d", id.Version, want)
	}
	return nil
}

func (s *orgState) reset(ctx context.Context, r *scenario.Reporter) error {
	s.client = httpx.NewClient(scenario.EnvFrom(ctx).Base)
	r.Note("This step posts the %q state to %s, so the database starts this run from a predictable, known set of data: the schema at its latest version and the seven-row acme tree below as the only rows, whatever the last run left behind.", seedState, statePath)
	root, err := repo.Root(ctx)
	if err != nil {
		return err
	}
	file := path.Join(seedsDir, seedState+".json")
	text, err := fs.ReadFile(os.DirFS(root), file)
	if err != nil {
		return err
	}
	r.JSON(file, text)
	body := map[string]string{"state": seedState}
	r.Request(http.MethodPost, statePath, nil, body)
	res, err := s.client.Post(ctx, statePath, body)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) list(ctx context.Context, r *scenario.Reporter) error {
	r.Note("A list request with no query component (no filter, no page, no size) returns the first page at the configured default size, %d results per page. The later steps take the ids and versions they need from this page, by code.", defaultPageSize)
	p, err := s.listPage(ctx, r)
	if err != nil {
		return err
	}
	if p.Size != defaultPageSize {
		return fmt.Errorf("the default page size is %d, not the %d the narration states", p.Size, defaultPageSize)
	}
	s.byCode = make(map[string]organization, len(p.Items))
	for _, o := range p.Items {
		s.byCode[o.Code] = o
	}
	return nil
}

// listPage is the raw list both list steps make: GET with no query,
// narrated, decoded.
func (s *orgState) listPage(ctx context.Context, r *scenario.Reporter) (page, error) {
	r.Request(http.MethodGet, organizationsPath, nil, nil)
	res, err := s.client.Get(ctx, organizationsPath)
	if err != nil {
		return page{}, err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return page{}, err
	}
	var p page
	if err := res.JSON(&p); err != nil {
		return page{}, err
	}
	return p, nil
}

func (s *orgState) query(ctx context.Context, r *scenario.Reporter) error {
	r.Note("A filter is a query parameter named for a field. The name alone tests equality; the name with a bracketed operator, field[op]=value, applies that operator." +
		"\n\nThe operators are:" +
		"\n- eq (equals)" +
		"\n- ne (not equals)" +
		"\n- gt (greater than)" +
		"\n- ge (greater than or equal)" +
		"\n- lt (less than)" +
		"\n- le (less than or equal)" +
		"\n- like" +
		"\n- null" +
		"\n- notnull" +
		"\n- in" +
		"\n\nA parameter repeated with no operator reads as in, and null and notnull take no value." +
		"\n\nThe organization read model declares eight filterable fields:" +
		"\n- id" +
		"\n- parent_id" +
		"\n- code" +
		"\n- name" +
		"\n- version" +
		"\n- created_at" +
		"\n- updated_at" +
		"\n- path" +
		"\n\npage selects a 1-based page and size its length, up to the configured maximum. sort takes comma-separated field names, each with a leading - for descending." +
		"\n\nHere, like passes its value to SQL's LIKE as given, so the caller supplies the wildcards: %%o%% matches every code containing an o, encoded as %%25o%%25 in the URL. Four codes match, and size=2 asks for them two to a page.")
	query := rawQuery([2]string{"code[like]", "%o%"}, [2]string{"size", "2"}, [2]string{"page", "1"})
	path := organizationsPath + "?" + query
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) find(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.seeded("acme")
	if err != nil {
		return err
	}
	path := organizationsPath + "/" + acme.ID
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) findByPath(ctx context.Context, r *scenario.Reporter) error {
	path := organizationsPath + "/path/acme/engineering/platform"
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) create(ctx context.Context, r *scenario.Reporter) error {
	env := scenario.EnvFrom(ctx)
	acme, err := s.seeded("acme")
	if err != nil {
		return err
	}
	r.Note("This creates %s under acme. The response's X-Request-Id header is the request's trace id. The trace becomes queryable in Tempo a few seconds after the response returns, once the service's exporter flushes its next batch and the collector batches it again; the trace id and where to find it in Grafana follow the response below.", createdCode)
	body := createOrganization{ParentID: &acme.ID, Code: createdCode, Name: createdName}
	r.Request(http.MethodPost, organizationsPath, nil, body)
	res, err := s.client.Post(ctx, organizationsPath, body)
	if err != nil {
		return err
	}
	r.Response(res)
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	if err := res.JSON(&s.created); err != nil {
		return err
	}
	traceID := res.Header.Get("X-Request-Id")
	if traceID == "" {
		return errors.New("the create response carries no X-Request-Id header")
	}
	if !isTraceID(traceID) {
		return fmt.Errorf("X-Request-Id = %q, which is not a trace id (32 lowercase hex characters)", traceID)
	}
	r.Trace(env.Grafana, serviceName, traceID)
	return nil
}

func (s *orgState) edit(ctx context.Context, r *scenario.Reporter) error {
	target, err := s.seeded(editedCode)
	if err != nil {
		return err
	}
	r.Note("Rename the seeded %s row's name column to %q. The If-Match header version must match the version column for the row in the database in order for the write to succeed. This facilitates optimistic concurrency and prevents simultaneous writes from resulting in an undesired state. The write increments the version when successful.", target.Code, editedName)
	path := organizationsPath + "/" + target.ID
	body := editOrganization{Code: target.Code, Name: editedName}
	headers := []httpx.Header{ifMatch(target.Version)}
	r.Request(http.MethodPut, path, headers, body)
	res, err := s.client.Put(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := identityOf(res, target.ID)
	if err != nil {
		return err
	}
	return expectVersion(id, target.Version+1)
}

func (s *orgState) transfer(ctx context.Context, r *scenario.Reporter) error {
	target, err := s.seeded(movedCode)
	if err != nil {
		return err
	}
	parent, err := s.seeded(newParentCode)
	if err != nil {
		return err
	}
	r.Note("A transfer is a structural move, not a rename: this makes the seeded %s row a child of %s instead of operations, so its path changes and its code and name do not. The same If-Match precondition guards it, at %s's own version, %d.", target.Code, parent.Code, target.Code, target.Version)
	path := organizationsPath + "/" + target.ID + "/transfer"
	body := map[string]*string{"parent_id": &parent.ID}
	headers := []httpx.Header{ifMatch(target.Version)}
	r.Request(http.MethodPost, path, headers, body)
	res, err := s.client.Post(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := identityOf(res, target.ID)
	if err != nil {
		return err
	}
	return expectVersion(id, target.Version+1)
}

func (s *orgState) listAgain(ctx context.Context, r *scenario.Reporter) error {
	r.Note("Retrieving the raw list data again shows how the commands have mutated the data from its original state.")
	p, err := s.listPage(ctx, r)
	if err != nil {
		return err
	}
	byCode := make(map[string]organization, len(p.Items))
	for _, o := range p.Items {
		byCode[o.Code] = o
	}
	created, ok := byCode[createdCode]
	if !ok || created.ID != s.created.ID {
		return fmt.Errorf("the list does not show the created row %s as id %s", createdCode, s.created.ID)
	}
	if edited := byCode[editedCode]; edited.Name != editedName {
		return fmt.Errorf("the list shows %s named %q, want %q", editedCode, edited.Name, editedName)
	}
	parent := s.byCode[newParentCode]
	if moved := byCode[movedCode]; moved.ParentID == nil || *moved.ParentID != parent.ID {
		return fmt.Errorf("the list does not show %s under %s", movedCode, newParentCode)
	}
	return nil
}

func (s *orgState) delete(ctx context.Context, r *scenario.Reporter) error {
	r.Note("Delete only the %s row that we created, ensuring If-Match aligns with the row version.", createdCode)
	path := organizationsPath + "/" + s.created.ID
	headers := []httpx.Header{ifMatch(s.created.Version)}
	r.Request(http.MethodDelete, path, headers, nil)
	res, err := s.client.Delete(ctx, path, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusNoContent)
}
