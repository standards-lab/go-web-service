package demo

import (
	"context"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/internal/api"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

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

func init() {
	scenario.Add(organizationScenario())
}

// orgState is what the steps hand forward: the client the first step binds,
// the seeded rows by code from the first raw list, and the identity the
// create step's response returns. Each command targets a different row,
// and no row is written twice, so every If-Match reads the version from
// where the row was last seen: the seeded rows at the version the list
// showed, the created row at the version create returned.
type orgState struct {
	client  *httpx.Client
	tree    api.Tree
	created api.Identity
}

func organizationScenario() scenario.Scenario {
	s := &orgState{}
	return scenario.Scenario{
		Name:    "domain",
		Summary: "The organization domain's full CRUD surface against the running service, reseeded from a known fixture each run, with the create request's trace located in Grafana (needs the full compose stack)",
		Needs: []scenario.Need{
			{What: "postgres", Task: "db-up"},
			{What: "the observability profile", Task: "otel-up"},
			{What: "the service", Task: "serve", Check: httpx.Live},
			{What: "Grafana", Task: "otel-up", Check: httpx.GrafanaLive},
		},
		Steps: []scenario.Step{
			{Intent: "Initialization", Action: s.initialization},
			{Intent: "Raw List", Action: s.rawList},
			{Intent: "Queried List", Action: s.queriedList},
			{Intent: "Find", Action: s.find},
			{Intent: "Find by Path", Action: s.findByPath},
			{Intent: "Create", Action: s.create},
			{Intent: "Edit", Action: s.edit},
			{Intent: "Transfer", Action: s.transfer},
			{Intent: "Results", Action: s.results},
			{Intent: "Delete", Action: s.delete},
		},
	}
}

func (s *orgState) initialization(ctx context.Context, r *scenario.Reporter) error {
	s.client = httpx.NewClient(env.FromContext(ctx).Base)
	return api.Reset(ctx, s.client, r)
}

func (s *orgState) rawList(ctx context.Context, r *scenario.Reporter) error {
	r.Note("A list request with no query component (no filter, no page, no size) returns the first page at the configured default size, %d results per page. The later steps take the ids and versions they need from this page, by code.", api.DefaultPageSize)
	p, err := api.List(ctx, s.client, r)
	if err != nil {
		return err
	}
	if p.Size != api.DefaultPageSize {
		return fmt.Errorf("the default page size is %d, not the %d the narration states", p.Size, api.DefaultPageSize)
	}
	s.tree = make(api.Tree, len(p.Items))
	for _, o := range p.Items {
		s.tree[o.Code] = o
	}
	return nil
}

func (s *orgState) queriedList(ctx context.Context, r *scenario.Reporter) error {
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
	query := httpx.RawQuery([2]string{"code[like]", "%o%"}, [2]string{"size", "2"}, [2]string{"page", "1"})
	path := api.Organizations + "?" + query
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) find(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	path := api.Organizations + "/" + acme.ID
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) findByPath(ctx context.Context, r *scenario.Reporter) error {
	path := api.Organizations + "/path/acme/engineering/platform"
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

func (s *orgState) create(ctx context.Context, r *scenario.Reporter) error {
	e := env.FromContext(ctx)
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("This creates %s under acme. The response's X-Request-Id header is the request's trace id. The trace becomes queryable in Tempo a few seconds after the response returns, once the service's exporter flushes its next batch and the collector batches it again; the trace id and where to find it in Grafana follow the response below.", createdCode)
	body := api.CreateOrganization{ParentID: &acme.ID, Code: createdCode, Name: createdName}
	r.Request(http.MethodPost, api.Organizations, nil, body)
	res, err := s.client.Post(ctx, api.Organizations, body)
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
	traceID, err := res.TraceID()
	if err != nil {
		return err
	}
	r.Trace(e.Grafana, api.ServiceName, traceID)
	return nil
}

func (s *orgState) edit(ctx context.Context, r *scenario.Reporter) error {
	target, err := s.tree.Get(editedCode)
	if err != nil {
		return err
	}
	r.Note("Rename the seeded %s row's name column to %q. The If-Match header version must match the version column for the row in the database in order for the write to succeed. This facilitates optimistic concurrency and prevents simultaneous writes from resulting in an undesired state. The write increments the version when successful.", target.Code, editedName)
	path := api.Organizations + "/" + target.ID
	body := api.EditOrganization{Code: target.Code, Name: editedName}
	headers := []httpx.Header{httpx.IfMatch(target.Version)}
	r.Request(http.MethodPut, path, headers, body)
	res, err := s.client.Put(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := api.IdentityOf(res, target.ID)
	if err != nil {
		return err
	}
	return api.ExpectVersion(id, target.Version+1)
}

func (s *orgState) transfer(ctx context.Context, r *scenario.Reporter) error {
	target, err := s.tree.Get(movedCode)
	if err != nil {
		return err
	}
	parent, err := s.tree.Get(newParentCode)
	if err != nil {
		return err
	}
	r.Note("A transfer is a structural move, not a rename: this makes the seeded %s row a child of %s instead of operations, so its path changes and its code and name do not. The same If-Match precondition guards it, at %s's own version, %d.", target.Code, parent.Code, target.Code, target.Version)
	path := api.Organizations + "/" + target.ID + "/transfer"
	body := map[string]*string{"parent_id": &parent.ID}
	headers := []httpx.Header{httpx.IfMatch(target.Version)}
	r.Request(http.MethodPost, path, headers, body)
	res, err := s.client.Post(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := api.IdentityOf(res, target.ID)
	if err != nil {
		return err
	}
	return api.ExpectVersion(id, target.Version+1)
}

func (s *orgState) results(ctx context.Context, r *scenario.Reporter) error {
	r.Note("Retrieving the raw list data again shows how the commands have mutated the data from its original state.")
	p, err := api.List(ctx, s.client, r)
	if err != nil {
		return err
	}
	after := make(api.Tree, len(p.Items))
	for _, o := range p.Items {
		after[o.Code] = o
	}
	created, ok := after[createdCode]
	if !ok || created.ID != s.created.ID {
		return fmt.Errorf("the list does not show the created row %s as id %s", createdCode, s.created.ID)
	}
	if edited := after[editedCode]; edited.Name != editedName {
		return fmt.Errorf("the list shows %s named %q, want %q", editedCode, edited.Name, editedName)
	}
	parent := s.tree[newParentCode]
	if moved := after[movedCode]; moved.ParentID == nil || *moved.ParentID != parent.ID {
		return fmt.Errorf("the list does not show %s under %s", movedCode, newParentCode)
	}
	return nil
}

func (s *orgState) delete(ctx context.Context, r *scenario.Reporter) error {
	r.Note("Delete only the %s row that we created, ensuring If-Match aligns with the row version.", createdCode)
	path := api.Organizations + "/" + s.created.ID
	headers := []httpx.Header{httpx.IfMatch(s.created.Version)}
	r.Request(http.MethodDelete, path, headers, nil)
	res, err := s.client.Delete(ctx, path, headers...)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusNoContent)
}
