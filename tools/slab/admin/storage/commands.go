package storage

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// Commands builds the storage command, one subcommand per endpoint. Each
// leaf calls newClient when it runs, not when the tree is built, for the
// reason the database command's do, and renders its reply through out.
func Commands(newClient func() *Client, out *output.Output) *cobra.Command {
	storage := &cobra.Command{
		Use:   "storage",
		Short: "Call the object store's admin endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	storage.AddCommand(
		out.FixedCommand("diagnostics", "Read the object store's readiness (a live probe), its container, and its key length bound", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return newClient().Diagnostics(ctx)
		}),
		out.FixedCommand("container", "Create the configured container, succeeding when it exists", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return newClient().Container(ctx)
		}),
	)
	return storage
}
