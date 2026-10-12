package demo

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Scenarios returns the demo scenarios in the order the command tree and the
// listing present them.
func Scenarios() []scenario.Scenario {
	return []scenario.Scenario{Compile(), Organization(), Problems(), Storage()}
}

// Commands builds the demo command on go-cli-sdk, with one subcommand per
// scenario, named for the scenario, in presentation order. The demo command
// declares with Use the nodes its scenarios read, src's; the scenarios
// construct their own HTTP clients from the environment src resolves,
// since each addresses the stack it needs by name. Run with no scenario or
// an unknown one, it is a usage error the dispatcher reports.
func Commands(src scenario.Source) *cli.Command {
	demo := &cli.Command{
		Name:    "demo",
		Summary: "Run one scenario",
	}
	for _, s := range Scenarios() {
		demo.Add(scenario.Command(s, src))
	}
	return demo.Use(src.Nodes...)
}
