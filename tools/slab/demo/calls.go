package demo

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// SeedState is the named state a scenario's first step resets to, and
// SeedsDir is where the service's states live, relative to the repository
// root: data/seeds/default.json is the seven-row acme tree.
const (
	SeedState = "default"
	SeedsDir  = "data/seeds"
)

// ServiceName is the service.name resource attribute the service exports,
// which Tempo and Loki index the trace under. The same string is stated in
// two other places: internal/app/telemetry.go's serviceName constant, which
// is unexported and so not importable, and
// compose/observability/otel-collector.yaml.
const ServiceName = "go-web-service"

// maxCommandBody is the byte limit domain/organization/handler.go's
// maxCommandBody puts on a command's request body; a body past it is
// answered with 413. It is a fixture value for the problems scenario, not
// a restated contract, so it lives here rather than in a domain package.
const maxCommandBody = 1 << 16

// stateRoute is the database admin service's reset, inside the /admin
// mount: the route database.Client.Reset sends to. Reset builds the request
// itself rather than calling that client, because the step narrates the
// literal request it sends, and that request belongs beside its narration.
const stateRoute = database.Database + "/state"

// Tree is the organizations of one list response by code, the form a
// scenario keeps the seeded rows in to target a later command by code.
type Tree map[string]organization.Organization

// Get returns the organization with code, or an error naming the code when
// the tree has none.
func (t Tree) Get(code string) (organization.Organization, error) {
	o, ok := t[code]
	if !ok {
		return organization.Organization{}, fmt.Errorf("the seeded tree has no organization with code %q", code)
	}
	return o, nil
}

// Reset posts SeedState to stateRoute, narrated: the note saying why, the
// seed file as authored, the request, and the response, checked to be 200.
// It is every stack-backed scenario's first step, so the database starts
// each run from the same known rows.
func Reset(ctx context.Context, c *httpx.Client, r *scenario.Reporter) error {
	r.Note("This step posts the %q state to %s, so the database starts this run from a predictable, known set of data: the schema at its latest version and the seven-row acme tree below as the only rows, whatever the last run left behind.", SeedState, stateRoute)
	fsys, err := repo.FS(ctx)
	if err != nil {
		return err
	}
	file := path.Join(SeedsDir, SeedState+".json")
	text, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}
	r.JSON(file, text)
	body := database.State{State: SeedState}
	r.Request(http.MethodPost, stateRoute, nil, body)
	res, err := c.Post(ctx, stateRoute, body)
	if err != nil {
		return err
	}
	r.Response(res)
	return res.Expect(http.StatusOK)
}

// List is the raw list: GET organization.Organizations with no query,
// narrated, checked to be 200, and decoded.
func List(ctx context.Context, c *httpx.Client, r *scenario.Reporter) (organization.Page, error) {
	r.Request(http.MethodGet, organization.Organizations, nil, nil)
	res, err := c.Get(ctx, organization.Organizations)
	if err != nil {
		return organization.Page{}, err
	}
	r.Response(res)
	if err := res.Expect(http.StatusOK); err != nil {
		return organization.Page{}, err
	}
	var p organization.Page
	if err := res.JSON(&p); err != nil {
		return organization.Page{}, err
	}
	return p, nil
}

// IdentityOf reads the Identity envelope off a command's response and checks
// it names the row the command targeted.
func IdentityOf(res *httpx.Response, wantID string) (organization.Identity, error) {
	var id organization.Identity
	if err := res.JSON(&id); err != nil {
		return organization.Identity{}, err
	}
	if id.ID != wantID {
		return organization.Identity{}, fmt.Errorf("the response names id %s, want %s", id.ID, wantID)
	}
	return id, nil
}

// ExpectVersion checks a command left its row at version want: one past the
// version its If-Match carried.
func ExpectVersion(id organization.Identity, want int64) error {
	if id.Version != want {
		return fmt.Errorf("the row is at version %d, want %d", id.Version, want)
	}
	return nil
}

// OversizedBody returns a command body over maxCommandBody, and the short
// stand-in a narration prints in its place: the payload is a create body
// whose code is maxCommandBody+1 bytes of filler, well-formed JSON that the
// service's bounded reader rejects before the decoder sees its end, and the
// printable form names the filler's length instead of printing it.
func OversizedBody() (payload []byte, printable string) {
	n := maxCommandBody + 1
	payload = []byte(`{"code":"` + strings.Repeat("a", n) + `"}`)
	printable = fmt.Sprintf(`{"code":"<%d bytes>"}`, n)
	return payload, printable
}
