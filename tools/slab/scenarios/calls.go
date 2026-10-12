package scenarios

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// seedState is the named state a scenario's first step resets to, and
// seedsDir is where the service's states live, relative to the repository
// root: data/seeds/default.json is the seven-row acme tree.
const (
	seedState = "default"
	seedsDir  = "data/seeds"
)

// serviceName is the service.name resource attribute the service exports,
// which Tempo and Loki index the trace under. The same string is stated in
// two other places: internal/app/telemetry.go's serviceName constant, which
// is unexported and so not importable, and
// compose/otel-collector/otel-collector.yaml.
const serviceName = "go-web-service"

// maxCommandBody is the byte limit domain/organization/handler.go's
// maxCommandBody puts on a command's request body; a body past it is
// answered with 413. It is a fixture value for the problems scenario, not
// a restated contract, so it lives here rather than in a domain package.
const maxCommandBody = 1 << 16

// stateRoute is the database admin service's reset, inside the /admin
// mount: the route database.Client.Reset sends to. reset builds the request
// itself rather than calling that client, because the step narrates the
// literal request it sends, and that request belongs beside its narration.
const stateRoute = database.Database + "/state"

// tree is the organizations of one list response by code, the form a
// scenario keeps the seeded rows in to target a later command by code.
type tree map[string]organization.Organization

// treeOf returns the organizations of p by code.
func treeOf(p organization.Page) tree {
	t := make(tree, len(p.Items))
	for _, o := range p.Items {
		t[o.Code] = o
	}
	return t
}

// get returns the organization with code, or an error naming the code when
// the tree has none.
func (t tree) get(code string) (organization.Organization, error) {
	o, ok := t[code]
	if !ok {
		return organization.Organization{}, fmt.Errorf("the seeded tree has no organization with code %q", code)
	}
	return o, nil
}

// reset posts seedState to stateRoute, narrated: the note saying why, the
// seed file as authored from the repository the repo node locates, the
// request, and the response, checked to be 200. It is the domain and
// problems scenarios' first step, so the database starts each run from the
// same known rows; a scenario that calls it declares the repo node.
func reset(ctx context.Context, n Nodes, inv *cli.Invocation, c *httpx.Client, r *scenario.Reporter) error {
	r.Note("This step posts the %q state to %s, so the database starts this run from a predictable, known set of data: the schema at its latest version and the seven-row acme tree below as the only rows, whatever the last run left behind.", seedState, stateRoute)
	fsys, err := repo.FS(inv.Get(n.Repo))
	if err != nil {
		return err
	}
	file := path.Join(seedsDir, seedState+".json")
	text, err := fs.ReadFile(fsys, file)
	if err != nil {
		return err
	}
	st := styleOf(n, inv)
	if err := showApart(r, file, output.JSON(st, text)); err != nil {
		return err
	}
	res, err := call(ctx, r, st, c, http.MethodPost, stateRoute, nil, database.Reset{State: seedState, Confirm: true})
	if err != nil {
		return err
	}
	return res.Expect(http.StatusOK)
}

// list is the raw list: GET organization.Organizations with no query,
// narrated, checked to be 200, and decoded.
func list(ctx context.Context, r *scenario.Reporter, st style.Style, c *httpx.Client) (organization.Page, error) {
	res, err := call(ctx, r, st, c, http.MethodGet, organization.Organizations, nil, nil)
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

// identityOf reads the Identity envelope off a command's response and
// checks it names the row the command targeted.
func identityOf(res *httpx.Response, wantID string) (organization.Identity, error) {
	var id organization.Identity
	if err := res.JSON(&id); err != nil {
		return organization.Identity{}, err
	}
	if id.ID != wantID {
		return organization.Identity{}, fmt.Errorf("the response names id %s, want %s", id.ID, wantID)
	}
	return id, nil
}

// expectVersion checks a command left its row at version want: one past
// the version its If-Match carried.
func expectVersion(id organization.Identity, want int64) error {
	if id.Version != want {
		return fmt.Errorf("the row is at version %d, want %d", id.Version, want)
	}
	return nil
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
