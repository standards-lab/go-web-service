package cli

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// demoCommand builds demo with one subcommand per registered scenario, named
// for the scenario.
func demoCommand(opts *options) *cobra.Command {
	demo := &cobra.Command{
		Use:   "demo <scenario>",
		Short: "Run one scenario",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, s := range scenario.All() {
		demo.AddCommand(scenarioCommand(opts, s))
	}
	return demo
}

func scenarioCommand(opts *options, s scenario.Scenario) *cobra.Command {
	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Summary,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := env.WithContext(cmd.Context(), opts.runEnv())
			r := scenario.NewReporter(cmd.OutOrStdout(), scenario.ColorEnabled(opts.noColor))
			return scenario.Run(ctx, s, r)
		},
	}
	if s.Flags != nil {
		s.Flags(cmd.Flags())
	}
	return cmd
}
