package scenarios

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// seedState is the named state a scenario's first step resets to, and
// seedsDir is where the service's states live, relative to the repository
// root: data/seeds/default.json is the seven-row acme tree.
const (
	seedState = "default"
	seedsDir  = "data/seeds"
)

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
	if err := show(r, file, output.JSON(st, text)); err != nil {
		return err
	}
	res, err := call(ctx, r, st, c, http.MethodPost, stateRoute, nil, database.Reset{State: seedState, Confirm: true})
	if err != nil {
		return err
	}
	return res.Expect(http.StatusOK)
}
