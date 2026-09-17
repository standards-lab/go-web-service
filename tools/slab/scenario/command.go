package scenario

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/env"
)

// Command builds the cobra command that runs s: it binds the environment
// runEnv resolves into the context, narrates through the reporter newReporter
// builds over the command's output, and adds s.Flags to the command's own
// flag set.
func Command(s Scenario, runEnv func() env.Env, newReporter func(io.Writer) *Reporter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Summary,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := env.WithContext(cmd.Context(), runEnv())
			return Run(ctx, s, newReporter(cmd.OutOrStdout()))
		},
	}
	if s.Flags != nil {
		s.Flags(cmd.Flags())
	}
	return cmd
}
