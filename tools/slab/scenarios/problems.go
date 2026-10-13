package scenarios

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// absentID is a well-formed id no row carries: valid UUID syntax, and never
// minted. integration/organization_test.go states the same id for the same
// purpose.
const absentID = "00000000-0000-7000-8000-000000000000"

// maxCommandBody is the byte limit domain/organization/handler.go's
// maxCommandBody puts on a command's request body; a body past it is
// answered with 413. It is a fixture value for the problems scenario, not
// a restated contract, so it lives here rather than in a domain package.
const maxCommandBody = 1 << 16

// The inputs the conditions send. invalidCode breaks the code rule twice
// over, with a capital and a space; malformedBody is a create body cut off
// before its closing brace, so it is not a JSON value at all.
const (
	invalidCode   = "Sales Team"
	malformedBody = `{"parent_id": null, "code": "sales", "name": "Sales"`
)

// The guarded commands' target: every If-Match condition sends the same
// rename of the seeded acme row, and none of them succeeds.
const renamedName = "Acme Corporation, Inc."

// problems is what the problems scenario's steps hand forward: the client
// and the seeded rows by code, both replaced by Initialization on every
// run. No condition writes a row, so every id and version a later step
// sends is the one the list showed.
type problems struct {
	n      Nodes
	client *httpx.Client
	tree   tree
}

// Problems returns the problems scenario over n: the service's
// problem-response contract (RFC 9457) across its real error conditions,
// one request each, with every response's trace located in Grafana. It
// declares the service and grafana up nodes, which it calls and points at,
// the repo node, which locates the seed file its reset shows, and the no
// color node, which decides its blocks' color.
func Problems(n Nodes) scenario.Scenario {
	s := &problems{n: n}
	return scenario.Scenario{
		Name:    "problems",
		Summary: "The service's problem-response contract (RFC 9457) across its real error conditions, one request each against the running service, with every response's trace located in Grafana",
		Nodes:   []graph.Ref{n.Service, n.GrafanaUp, n.Repo, n.NoColor},
		Steps: []scenario.Step{
			{Intent: "Initialization", Action: s.initialization},
			{Intent: "Malformed Identifier", Action: s.malformedIdentifier},
			{Intent: "Malformed Query", Action: s.malformedQuery},
			{Intent: "Malformed Body", Action: s.malformedBody},
			{Intent: "Oversized Body", Action: s.oversizedBody},
			{Intent: "Domain Validation", Action: s.domainValidation},
			{Intent: "Missing If-Match", Action: s.missingIfMatch},
			{Intent: "Malformed If-Match", Action: s.malformedIfMatch},
			{Intent: "Stale Version", Action: s.staleVersion},
			{Intent: "Not Found", Action: s.notFound},
			{Intent: "Duplicate Code", Action: s.duplicateCode},
			{Intent: "Transfer Cycle", Action: s.transferCycle},
		},
	}
}

func (s *problems) initialization(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	s.client = inv.Get(s.n.Service).Client
	if err := reset(ctx, s.n, inv, s.client, r); err != nil {
		return err
	}
	p, err := s.list(ctx)
	if err != nil {
		return err
	}
	s.tree = treeOf(p)
	return nil
}

// list fetches the seeded tree without narrating it: the ids and versions
// are bookkeeping for the conditions that follow, not something this
// scenario demonstrates.
func (s *problems) list(ctx context.Context) (organization.Page, error) {
	res, err := s.client.Get(ctx, organization.Organizations)
	if err != nil {
		return organization.Page{}, err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return organization.Page{}, err
	}
	var p organization.Page
	if err := res.JSON(&p); err != nil {
		return organization.Page{}, err
	}
	return p, nil
}

func (s *problems) malformedIdentifier(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("Every route with an {id} segment is first parsed as a UUID. A segment that is not a valid UUID results in a 400 response, with the detail specifying the segment. The query never reaches the database.")
	return s.observe(ctx, inv, r, http.MethodGet, organization.Organizations+"/not-a-uuid", nil, nil, http.StatusBadRequest)
}

func (s *problems) malformedQuery(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("The list parses its query string whole: page and size must be integers of at least 1, size at most the configured maximum, and sort a comma-separated list of field names. page=0 breaks the first rule, since pages are numbered from 1, and the parser answers 400 with the parameter, the value, and the rule in the detail." +
		"\n\nAn operator the read model does not support, code[between]=a for instance, is also a 400, but from a different place: the query parser passes an operator through as text, and the data layer rejects it when it lowers the filter into a directive against the read model. The detail names the operator the same way.")
	path := organization.Organizations + "?" + httpx.RawQuery([2]string{"page", "0"})
	return s.observe(ctx, inv, r, http.MethodGet, path, nil, nil, http.StatusBadRequest)
}

func (s *problems) malformedBody(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A command body is decoded strictly as one JSON value. The following incorrect body formats result in a 400 with a detail indicating the decoder's reason it is invalid:" +
		"\n\n- malformed JSON" +
		"\n- a field not declared by the command" +
		"\n- more than one JSON value" +
		"\n- an empty body" +
		"\n\nThis create body stops before its closing brace, resulting in invalid JSON.")
	return s.observe(ctx, inv, r, http.MethodPost, organization.Organizations, nil, malformedBody, http.StatusBadRequest)
}

func (s *problems) oversizedBody(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	payload, printable := oversizedBody()
	r.Note("Commands are typically only a JSON payload with a few fields, so the handler bounds a command body at %d bytes. This create body is well-formed JSON whose code is %d bytes of filler. The bounded reader stops at the limit before the decoder sees the end of the value, and the response is a 413 naming the limit. The request below prints the filler's length in place of the filler.", maxCommandBody, maxCommandBody+1)
	st := styleOf(s.n, inv)
	if err := showApart(r, "", output.Request(st, http.MethodPost, organization.Organizations, nil, printable)); err != nil {
		return err
	}
	res, err := s.client.Post(ctx, organization.Organizations, payload)
	if err != nil {
		return err
	}
	if err := showApart(r, "", output.Response(st, res)); err != nil {
		return err
	}
	return s.answered(r, st, inv, res, http.StatusRequestEntityTooLarge)
}

func (s *problems) domainValidation(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("After a body is decoded, the command is validated. When posting a new organization, the following rules apply:"+
		"\n\n- a code must be lowercase words joined by single hyphens"+
		"\n- a name must not be empty"+
		"\n- parent_id, when present, must be a UUID"+
		"\n\nThe rules mirror the schema's check constraints, and an invalid state results in a 400 and a detail of the invalid state. This command attempts to create an organization with the code %q, which has capital letters and a space.", invalidCode)
	body := organization.CreateOrganization{ParentID: &acme.ID, Code: invalidCode, Name: "Sales Team"}
	return s.observe(ctx, inv, r, http.MethodPost, organization.Organizations, nil, body, http.StatusBadRequest)
}

func (s *problems) missingIfMatch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("Edit, transfer, and delete are guarded commands. They mutate the state of existing data, so they must specify an If-Match header set to the value of the most recently retrieved row version. This prevents two callers from simultaneously writing to the same row (optimistic concurrency). A guarded command with no If-Match results in a 428 Precondition Required response, and is refused before the body is read.")
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	return s.observe(ctx, inv, r, http.MethodPut, organization.Organizations+"/"+acme.ID, nil, body, http.StatusPreconditionRequired)
}

func (s *problems) malformedIfMatch(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("If-Match must be exactly one strong entity-tag whose value is the integer version, \"%d\" here. A weak tag, W/\"%d\", presents an invalid form the guard cannot compare. RFC 9110 requires strong comparison for If-Match to succeed, so the header syntax fails. This results in a 400 response with the detail articulating the error.", acme.Version, acme.Version)
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	headers := []httpx.Header{{Name: "If-Match", Value: fmt.Sprintf(`W/"%d"`, acme.Version)}}
	return s.observe(ctx, inv, r, http.MethodPut, organization.Organizations+"/"+acme.ID, headers, body, http.StatusBadRequest)
}

func (s *problems) staleVersion(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("A well-formed If-Match with an out of date version results in a 412 Precondition Failed response. The guarded UPDATE matches no row at that version, so the write does not happen. The response does not include a detail because there is nothing about the request to fix.")
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	headers := []httpx.Header{httpx.IfMatch(acme.Version + 1)}
	return s.observe(ctx, inv, r, http.MethodPut, organization.Organizations+"/"+acme.ID, headers, body, http.StatusPreconditionFailed)
}

func (s *problems) notFound(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	r.Note("A well-formed ID that doesn't match any row results in a 404 response without a detail. The 404 Not Found response provides all of the context needed.")
	return s.observe(ctx, inv, r, http.MethodGet, organization.Organizations+"/"+absentID, nil, nil, http.StatusNotFound)
}

func (s *problems) duplicateCode(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	r.Note("The schema's unique constraint on (parent_id, code) means that a code cannot be duplicated within the same parent_id value. Creating a second %q organization at the root collides with the seeded %q organization and results in a 409 Conflict. A parent_id that does not point to an existing record is the same problem class and also results in a 409 Conflict response. The command is technically valid, but the database constraint is violated and the error is raised when a write is attempted.", acme.Code, acme.Code)
	body := organization.CreateOrganization{ParentID: nil, Code: acme.Code, Name: acme.Name}
	return s.observe(ctx, inv, r, http.MethodPost, organization.Organizations, nil, body, http.StatusConflict)
}

func (s *problems) transferCycle(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	acme, err := s.tree.get("acme")
	if err != nil {
		return err
	}
	platform, err := s.tree.get("platform")
	if err != nil {
		return err
	}
	r.Note("A transfer moves an organization under a new parent. Attempting to transfer an organization to itself or any element within its current hierarchy will result in a 409 Conflict response. Allowing this would essentially break the organizational hierarchy by orphaning the organization from the rest of the organizational structure. When trying to move /acme to /acme/engineering/platform, the cycle check identifies and refuses the attempted transfer cycle.")
	body := map[string]*string{"parent_id": &platform.ID}
	headers := []httpx.Header{httpx.IfMatch(acme.Version)}
	return s.observe(ctx, inv, r, http.MethodPost, organization.Organizations+"/"+acme.ID+"/transfer", headers, body, http.StatusConflict)
}

// observe is every condition's call but the oversized body's, whose
// request shows a stand-in for what it sends: it sends the request,
// narrated, and checks the answer with [problems.answered].
func (s *problems) observe(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter, method, path string, headers []httpx.Header, body any, status int) error {
	st := styleOf(s.n, inv)
	res, err := call(ctx, r, st, s.client, method, path, headers, body)
	if err != nil {
		return err
	}
	return s.answered(r, st, inv, res, status)
}

// answered is every condition's ending, after its response is shown: it
// checks the response is a problem document answering with status, and
// points at the request's trace in Grafana, read from the response's
// X-Request-Id header.
func (s *problems) answered(r *scenario.Reporter, st style.Style, inv *cli.Invocation, res *httpx.Response, status int) error {
	if _, err := res.Problem(status); err != nil {
		return err
	}
	return showTrace(r, st, inv.Get(s.n.GrafanaUp), res)
}

// oversizedBody returns a command body over maxCommandBody, and the short
// stand-in a narration shows in its place: the payload is a create body
// whose code is maxCommandBody+1 bytes of filler, well-formed JSON that the
// service's bounded reader rejects before the decoder sees its end, and the
// printable form names the filler's length instead of carrying it.
func oversizedBody() (payload []byte, printable string) {
	n := maxCommandBody + 1
	payload = []byte(`{"code":"` + strings.Repeat("a", n) + `"}`)
	printable = fmt.Sprintf(`{"code":"<%d bytes>"}`, n)
	return payload, printable
}
