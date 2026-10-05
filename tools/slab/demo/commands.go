package demo

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Scenarios returns the demo scenarios in the order the command tree and the
// listing present them.
func Scenarios() []scenario.Scenario {
	return []scenario.Scenario{Compile(), Organization(), Problems(), Storage()}
}

// Commands builds the demo command with one subcommand per scenario, named
// for the scenario.
func Commands(runEnv func() env.Env, newReporter func(io.Writer) *scenario.Reporter) *cobra.Command {
	demo := &cobra.Command{
		Use:   "demo <scenario>",
		Short: "Run one scenario",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, s := range Scenarios() {
		demo.AddCommand(scenario.Command(s, runEnv, newReporter))
	}
	return demo
}
