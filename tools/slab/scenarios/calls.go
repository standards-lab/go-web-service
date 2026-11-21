package scenarios

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

const (
	seedState = "default"
	seedsDir  = "data/seeds"
)

const fixturesDir = seedsDir + "/fixtures"

const sweepLimit = 45 * time.Second

const stateRoute = database.Database + "/state"

type tree map[string]organization.Organization

func treeOf(p organization.Page) tree {
	t := make(tree, len(p.Items))
	for _, o := range p.Items {
		t[o.Code] = o
	}
	return t
}

func (t tree) get(code string) (organization.Organization, error) {
	o, ok := t[code]
	if !ok {
		return organization.Organization{}, fmt.Errorf("the seeded tree has no organization with code %q", code)
	}
	return o, nil
}

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

func lookup(ctx context.Context, c *httpx.Client, orgPath string) (organization.Organization, error) {
	res, err := c.Get(ctx, organization.Organizations+"/lookup?"+httpx.RawQuery([2]string{"path", orgPath}))
	if err != nil {
		return organization.Organization{}, err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return organization.Organization{}, fmt.Errorf("lookup %s: %w", orgPath, err)
	}
	var o organization.Organization
	if err := res.JSON(&o); err != nil {
		return organization.Organization{}, err
	}
	return o, nil
}

func logoRoute(org string) string {
	return organization.Organizations + "/" + org + "/logo"
}

func documentsRoute(org string) string {
	return document.Documents + "/" + org
}

func directoryRoute(org, id string) string {
	return documentsRoute(org) + "/directories/" + id
}

func createDirectory(ctx context.Context, r *scenario.Reporter, st style.Style, c *httpx.Client, route string, body document.CreateDirectory) (document.Identity, error) {
	res, err := call(ctx, r, st, c, http.MethodPost, route, nil, body)
	if err != nil {
		return document.Identity{}, err
	}
	if err := res.Expect(http.StatusCreated); err != nil {
		return document.Identity{}, err
	}
	var id document.Identity
	if err := res.JSON(&id); err != nil {
		return document.Identity{}, err
	}
	return id, nil
}
