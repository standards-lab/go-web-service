package storage

import (
	"context"
	"net/http"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// Commands builds the storage command, on go-cli-sdk, with one subcommand
// per endpoint. The storage command declares with Use out's configuration
// node and client, the HTTP client bound to the service's base URL, as the
// database command does; neither endpoint takes input, so a leaf accepts
// no arguments, and its Run sends its one request, checks for 200 with
// output.Expect, and renders the reply through out.
func Commands(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	fixed := func(name, summary string, op func(*Client, context.Context) (*httpx.Response, error)) *cli.Command {
		return &cli.Command{
			Name:    name,
			Summary: summary,
			Args:    cli.NoArgs,
			Run: func(ctx context.Context, inv *cli.Invocation) error {
				res, err := op(NewClient(inv.Get(client)), ctx)
				if err != nil {
					return err
				}
				if err := output.Expect(res, http.StatusOK); err != nil {
					return err
				}
				out.New(inv).Response(res.Status, res.Body)
				return nil
			},
		}
	}
	storage := &cli.Command{
		Name:    "storage",
		Summary: "Call the object store's admin endpoints",
	}
	return storage.Add(
		fixed("diagnostics", "Read the object store's readiness (a live probe), its container, and its key length bound", (*Client).Diagnostics),
		fixed("container", "Create the configured container, succeeding when it exists", (*Client).Container),
	).Use(out.Node, client)
}
