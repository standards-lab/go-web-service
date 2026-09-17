package cli

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/internal/demo"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the scenarios and what each one needs running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scenario.WriteListing(cmd.OutOrStdout(), demo.Scenarios())
			return nil
		},
	}
}
