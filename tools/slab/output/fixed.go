package output

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// FixedCommand builds the subcommand for an endpoint that takes no input:
// use and short name it, and its RunE sends the one request op stands for,
// checks for status through Expect, and prints the reply through Response.
// It accepts no arguments. op is the shape every domain client's no-input
// method has once its receiver is bound, so a caller passes a closure that
// constructs the client and calls the method, and the client is constructed
// when the command runs, not when the tree is built.
func FixedCommand(use, short string, status int, op func(ctx context.Context) (*httpx.Response, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := op(cmd.Context())
			if err != nil {
				return err
			}
			if err := Expect(res, status); err != nil {
				return err
			}
			Response(cmd.OutOrStdout(), res.Status, res.Body)
			return nil
		},
	}
}
