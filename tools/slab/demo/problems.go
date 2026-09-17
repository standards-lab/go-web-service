package demo

import (
	"context"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// absentID is a well-formed id no row carries: valid UUID syntax, and never
// minted. integration/organization_test.go states the same id for the same
// purpose.
const absentID = "00000000-0000-7000-8000-000000000000"

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

// problemState is what the steps hand forward: the client and the seeded
// rows by code, both bound by Initialization. No condition writes a row, so
// every id and version a later step sends is the one the list showed.
type problemState struct {
	client *httpx.Client
	tree   Tree
}

// Problems returns the problems scenario over fresh state.
func Problems() scenario.Scenario {
	s := &problemState{}
	return scenario.Scenario{
		Name:    "problems",
		Summary: "The service's problem-response contract (RFC 9457) across its real error conditions, one request each against the running service, with every response's trace located in Grafana (needs the full compose stack)",
		Needs: []scenario.Need{
			{What: "postgres", Task: "db-up"},
			{What: "the observability profile", Task: "otel-up"},
			{What: "the service", Task: "serve", Check: httpx.Live},
			{What: "Grafana", Task: "otel-up", Check: httpx.GrafanaLive},
		},
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

func (s *problemState) initialization(ctx context.Context, r *scenario.Reporter) error {
	s.client = httpx.NewClient(env.FromContext(ctx).Base)
	if err := Reset(ctx, s.client, r); err != nil {
		return err
	}
	p, err := s.list(ctx)
	if err != nil {
		return err
	}
	s.tree = make(Tree, len(p.Items))
	for _, o := range p.Items {
		s.tree[o.Code] = o
	}
	return nil
}

// list fetches the seeded tree without narrating it: the ids and versions
// are bookkeeping for the conditions that follow, not something this
// scenario demonstrates.
func (s *problemState) list(ctx context.Context) (organization.Page, error) {
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

func (s *problemState) malformedIdentifier(ctx context.Context, r *scenario.Reporter) error {
	r.Note("Every route with an {id} segment is first parsed as a UUID. A segment that is not a valid UUID results in a 400 response, with the detail specifying the segment. The query never reaches the database.")
	path := organization.Organizations + "/not-a-uuid"
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusBadRequest)
}

func (s *problemState) malformedQuery(ctx context.Context, r *scenario.Reporter) error {
	r.Note("The list parses its query string whole: page and size must be integers of at least 1, size at most the configured maximum, and sort a comma-separated list of field names. page=0 breaks the first rule, since pages are numbered from 1, and the parser answers 400 with the parameter, the value, and the rule in the detail." +
		"\n\nAn operator the read model does not support, code[between]=a for instance, is also a 400, but from a different place: the query parser passes an operator through as text, and the data layer rejects it when it lowers the filter into a directive against the read model. The detail names the operator the same way.")
	path := organization.Organizations + "?" + httpx.RawQuery([2]string{"page", "0"})
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusBadRequest)
}

func (s *problemState) malformedBody(ctx context.Context, r *scenario.Reporter) error {
	r.Note("A command body is decoded strictly as one JSON value. The following incorrect body formats result in a 400 with a detail indicating the decoder's reason it is invalid:" +
		"\n\n- malformed JSON" +
		"\n- a field not declared by the command" +
		"\n- more than one JSON value" +
		"\n- an empty body" +
		"\n\nThis create body stops before its closing brace, resulting in invalid JSON.")
	r.Request(http.MethodPost, organization.Organizations, nil, malformedBody)
	res, err := s.client.Post(ctx, organization.Organizations, malformedBody)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusBadRequest)
}

func (s *problemState) oversizedBody(ctx context.Context, r *scenario.Reporter) error {
	payload, printable := OversizedBody()
	r.Note("Commands are typically only a JSON payload with a few fields, so the handler bounds a command body at %d bytes. This create body is well-formed JSON whose code is %d bytes of filler. The bounded reader stops at the limit before the decoder sees the end of the value, and the response is a 413 naming the limit. The request below prints the filler's length in place of the filler.", maxCommandBody, maxCommandBody+1)
	r.Request(http.MethodPost, organization.Organizations, nil, printable)
	res, err := s.client.Post(ctx, organization.Organizations, payload)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusRequestEntityTooLarge)
}

func (s *problemState) domainValidation(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("After a body is decoded, the command is validated. When posting a new organization, the following rules apply:"+
		"\n\n- a code must be lowercase words joined by single hyphens"+
		"\n- a name must not be empty"+
		"\n- parent_id, when present, must be a UUID"+
		"\n\nThe rules mirror the schema's check constraints, and an invalid state results in a 400 and a detail of the invalid state. This command attempts to create an organization with the code %q, which has capital letters and a space.", invalidCode)
	body := organization.CreateOrganization{ParentID: &acme.ID, Code: invalidCode, Name: "Sales Team"}
	r.Request(http.MethodPost, organization.Organizations, nil, body)
	res, err := s.client.Post(ctx, organization.Organizations, body)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusBadRequest)
}

func (s *problemState) missingIfMatch(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("Edit, transfer, and delete are guarded commands. They mutate the state of existing data, so they must specify an If-Match header set to the value of the most recently retrieved row version. This prevents two callers from simultaneously writing to the same row (optimistic concurrency). A guarded command with no If-Match results in a 428 Precondition Required response, and is refused before the body is read.")
	path := organization.Organizations + "/" + acme.ID
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	r.Request(http.MethodPut, path, nil, body)
	res, err := s.client.Put(ctx, path, body)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusPreconditionRequired)
}

func (s *problemState) malformedIfMatch(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("If-Match must be exactly one strong entity-tag whose value is the integer version, \"%d\" here. A weak tag, W/\"%d\", presents an invalid form the guard cannot compare. RFC 9110 requires strong comparison for If-Match to succeed, so the header syntax fails. This results in a 400 response with the detail articulating the error.", acme.Version, acme.Version)
	path := organization.Organizations + "/" + acme.ID
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	headers := []httpx.Header{{Name: "If-Match", Value: fmt.Sprintf(`W/"%d"`, acme.Version)}}
	r.Request(http.MethodPut, path, headers, body)
	res, err := s.client.Put(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusBadRequest)
}

func (s *problemState) staleVersion(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("A well-formed If-Match with an out of date version results in a 412 Precondition Failed response. The guarded UPDATE matches no row at that version, so the write does not happen. The response does not include a detail because there is nothing about the request to fix.")
	path := organization.Organizations + "/" + acme.ID
	body := organization.EditOrganization{Code: acme.Code, Name: renamedName}
	headers := []httpx.Header{httpx.IfMatch(acme.Version + 1)}
	r.Request(http.MethodPut, path, headers, body)
	res, err := s.client.Put(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusPreconditionFailed)
}

func (s *problemState) notFound(ctx context.Context, r *scenario.Reporter) error {
	r.Note("A well-formed ID that doesn't match any row results in a 404 response without a detail. The 404 Not Found response provides all of the context needed.")
	path := organization.Organizations + "/" + absentID
	r.Request(http.MethodGet, path, nil, nil)
	res, err := s.client.Get(ctx, path)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusNotFound)
}

func (s *problemState) duplicateCode(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	r.Note("The schema's unique constraint on (parent_id, code) means that a code cannot be duplicated within the same parent_id value. Creating a second %q organization at the root collides with the seeded %q organization and results in a 409 Conflict. A parent_id that does not point to an existing record is the same problem class and also results in a 409 Conflict response. The command is technically valid, but the database constraint is violated and the error is raised when a write is attempted.", acme.Code, acme.Code)
	body := organization.CreateOrganization{ParentID: nil, Code: acme.Code, Name: acme.Name}
	r.Request(http.MethodPost, organization.Organizations, nil, body)
	res, err := s.client.Post(ctx, organization.Organizations, body)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusConflict)
}

func (s *problemState) transferCycle(ctx context.Context, r *scenario.Reporter) error {
	acme, err := s.tree.Get("acme")
	if err != nil {
		return err
	}
	platform, err := s.tree.Get("platform")
	if err != nil {
		return err
	}
	r.Note("A transfer moves an organization under a new parent. Attempting to transfer an organization to itself or any element within its current hierarchy will result in a 409 Conflict response. Allowing this would essentially break the organizational hierarchy by orphaning the organization from the rest of the organizational structure. When trying to move /acme to /acme/engineering/platform, the cycle check identifies and refuses the attempted transfer cycle.")
	path := organization.Organizations + "/" + acme.ID + "/transfer"
	body := map[string]*string{"parent_id": &platform.ID}
	headers := []httpx.Header{httpx.IfMatch(acme.Version)}
	r.Request(http.MethodPost, path, headers, body)
	res, err := s.client.Post(ctx, path, body, headers...)
	if err != nil {
		return err
	}
	return s.observe(ctx, r, res, http.StatusConflict)
}

// observe is every condition's ending: it prints the response, checks it is
// a problem document answering with status, and points at the request's
// trace in Grafana, read from the response's X-Request-Id header.
func (s *problemState) observe(ctx context.Context, r *scenario.Reporter, res *httpx.Response, status int) error {
	r.Response(res)
	if _, err := res.Problem(status); err != nil {
		return err
	}
	traceID, err := res.TraceID()
	if err != nil {
		return err
	}
	r.Trace(env.FromContext(ctx).Grafana, ServiceName, traceID)
	return nil
}
