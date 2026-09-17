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
	return []scenario.Scenario{Compile(), Organization(), Problems()}
}

// Commands builds the demo command with one subcommand per scenario, named
// for the scenario.
func Commands(runEnv func() env.Env, newReporter func(io.Writer) *scenario.Reporter) *cobra.Command {
	return commands(Scenarios(), runEnv, newReporter)
}

// commands is Commands over an explicit list, so a test can hand it
// duplicates. Two scenarios sharing a name panics, since the command tree
// would otherwise carry two subcommands that answer to the same word.
func commands(all []scenario.Scenario, runEnv func() env.Env, newReporter func(io.Writer) *scenario.Reporter) *cobra.Command {
	demo := &cobra.Command{
		Use:   "demo <scenario>",
		Short: "Run one scenario",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	seen := make(map[string]bool, len(all))
	for _, s := range all {
		if seen[s.Name] {
			panic("demo: scenario " + s.Name + " listed twice")
		}
		seen[s.Name] = true
		demo.AddCommand(scenario.Command(s, runEnv, newReporter))
	}
	return demo
}
