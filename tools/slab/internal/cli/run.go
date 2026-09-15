package cli

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// runCommand builds run with one subcommand per registered scenario, named
// for the scenario. cobra matches command names literally, so a name such as
// sqlate:compile is one word to it.
func runCommand(opts *options) *cobra.Command {
	run := &cobra.Command{
		Use:   "run <scenario>",
		Short: "Run one scenario",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, s := range scenario.All() {
		run.AddCommand(scenarioCommand(opts, s))
	}
	return run
}

func scenarioCommand(opts *options, s scenario.Scenario) *cobra.Command {
	cmd := &cobra.Command{
		Use:   s.Name,
		Short: s.Summary,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := scenario.WithEnv(cmd.Context(), opts.env())
			r := scenario.NewReporter(cmd.OutOrStdout(), scenario.ColorEnabled(opts.noColor))
			return scenario.Run(ctx, s, r)
		},
	}
	if s.Flags != nil {
		s.Flags(cmd.Flags())
	}
	return cmd
}
