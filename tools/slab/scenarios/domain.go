package scenarios

import (
	"context"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/input"
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

// domain is what the domain scenario's steps hand forward: the client the
// first step binds, the seeded rows by code from the first raw list, and
// the identity the create step's response returns. Each command targets a
// different row, and no row is written twice, so every If-Match reads the
// version from where the row was last seen: the seeded rows at the version
// the list showed, the created row at the version create returned. The
// first two steps replace the client and the rows, so nothing a run reads
// is left from a run before it.
type domain struct {
	n       Nodes
	client  *httpx.Client
	tree    tree
	created organization.Identity
}

// Domain returns the domain scenario over n: the organization domain's
// full CRUD surface against the running service, reseeded from a known
// fixture each run, with the create request's trace located in Grafana.
// It declares the service and grafana up nodes, which it calls and points
// at, the repo node, which locates the seed file its reset shows, and the
// no color node, which decides its blocks' color.
func Domain(n Nodes) scenario.Scenario {
	s := &domain{n: n}
	return scenario.Scenario{
		Name:    "domain",
		Summary: "The organization domain's full CRUD surface against the running service, reseeded from a known fixture each run, with the create request's trace located in Grafana",
		Nodes:   []graph.Ref{n.Service, n.GrafanaUp, n.Repo, n.NoColor},
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

func (s *domain) initialization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	s.client = httpx.NewClient(inv.Get(s.n.Service).URL)
	return reset(ctx, s.n, inv, s.client, r)
}

func (s *domain) rawList(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A list request with no query component (no filter, no page, no size) returns the first page at the configured default size, %d results per page. The later steps take the ids and versions they need from this page, by code.", input.DefaultPageSize)
	p, err := list(ctx, r, styleOf(s.n, inv), s.client)
	if err != nil {
		return err
	}
	if p.Size != input.DefaultPageSize {
		return fmt.Errorf("the default page size is %d, not the %d the narration states", p.Size, input.DefaultPageSize)
	}
	s.tree = treeOf(p)
	return nil
}

func (s *domain) queriedList(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
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
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodGet, organization.Organizations+"?"+query, nil, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusOK)
}

func (s *domain) find(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodGet, organization.Organizations+"/"+acme.ID, nil, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusOK)
}

func (s *domain) findByPath(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodGet, organization.Organizations+"/lookup?path=/acme/engineering/platform", nil, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusOK)
}

func (s *domain) create(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("This creates %s under acme. The response's X-Request-Id header is the request's trace id. The trace becomes queryable in Tempo a few seconds after the response returns, once the service's exporter flushes its next batch and the collector batches it again; the trace id and where to find it in Grafana follow the response below.", createdCode)
	st := styleOf(s.n, inv)
	body := organization.CreateOrganization{ParentID: &acme.ID, Code: createdCode, Name: createdName}
	res, err := call(ctx, r, st, s.client, http.MethodPost, organization.Organizations, nil, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return err
	}
	if err := res.JSON(&s.created); err != nil {
		return err
	}
	return showTrace(r, st, inv.Get(s.n.GrafanaUp), res)
}

func (s *domain) edit(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	target, err := s.tree.get(editedCode)
	if err != nil {
		return err
	}
	r.Note("Rename the seeded %s row's name column to %q. The If-Match header version must match the version column for the row in the database in order for the write to succeed. This facilitates optimistic concurrency and prevents simultaneous writes from resulting in an undesired state. The write increments the version when successful.", target.Code, editedName)
	body := organization.EditOrganization{Code: target.Code, Name: editedName}
	headers := []httpx.Header{httpx.IfMatch(target.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPut, organization.Organizations+"/"+target.ID, headers, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := identityOf(res, target.ID)
	if err != nil {
		return err
	}
	return expectVersion(id, target.Version+1)
}

func (s *domain) transfer(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	target, err := s.tree.get(movedCode)
	if err != nil {
		return err
	}
	parent, err := s.tree.get(newParentCode)
	if err != nil {
		return err
	}
	r.Note("A transfer is a structural move, not a rename: this makes the seeded %s row a child of %s instead of operations, so its path changes and its code and name do not. The same If-Match precondition guards it, at %s's own version, %d.", target.Code, parent.Code, target.Code, target.Version)
	body := map[string]*string{"parent_id": &parent.ID}
	headers := []httpx.Header{httpx.IfMatch(target.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodPost, organization.Organizations+"/"+target.ID+"/transfer", headers, body)
	if err != nil {
		return err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return err
	}
	id, err := identityOf(res, target.ID)
	if err != nil {
		return err
	}
	return expectVersion(id, target.Version+1)
}

func (s *domain) results(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("Retrieving the raw list data again shows how the commands have mutated the data from its original state.")
	p, err := list(ctx, r, styleOf(s.n, inv), s.client)
	if err != nil {
		return err
	}
	after := treeOf(p)
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

func (s *domain) delete(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("Delete only the %s row that we created, ensuring If-Match aligns with the row version.", createdCode)
	headers := []httpx.Header{httpx.IfMatch(s.created.Version)}
	res, err := call(ctx, r, styleOf(s.n, inv), s.client, http.MethodDelete, organization.Organizations+"/"+s.created.ID, headers, nil)
	if err != nil {
		return err
	}
	return res.Expect(http.StatusNoContent)
}
