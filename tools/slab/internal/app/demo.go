package app

import (
	"context"
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/demo"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// demoCommand builds the demo command on the tree over the nodes from n its
// scenarios read: the base, grafana, tempo, and repo nodes, which make up
// the environment each scenario runs with on slab's own scenario runner,
// and the no color node, for the reporter it narrates through over the
// invocation's stdout, colored when stdout is a terminal and neither
// NO_COLOR nor the no color node says otherwise. The scenarios construct
// their own HTTP clients from that environment rather than through the
// client node, since each one addresses the stack it needs (the service,
// Grafana, Tempo) by name.
func demoCommand(n *Nodes) *cli.Command {
	base, grafana, tempo, repo, noColor := n.Base, n.Grafana, n.Tempo, n.Repo, n.NoColor
	return demo.Commands(scenario.Source{
		Nodes: []graph.Ref{base, grafana, tempo, repo, noColor},
		Env: func(inv *cli.Invocation) env.Env {
			return env.Env{Base: inv.Get(base), Grafana: inv.Get(grafana), Tempo: inv.Get(tempo), Repo: inv.Get(repo)}
		},
		Reporter: func(inv *cli.Invocation) *scenario.Reporter {
			return scenario.NewReporter(inv.Stdout, style.ColorEnabled(inv.Stdout, inv.Get(noColor)))
		},
	})
}

// listCommand builds list, which prints the scenario listing. It declares
// no nodes, so it builds nothing and reads no configuration.
func listCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Summary: "List the scenarios and what each one needs running",
		Args:    cli.NoArgs,
		Run: func(_ context.Context, inv *cli.Invocation) error {
			writeScenarios(inv.Stdout)
			return nil
		},
	}
}

// writeScenarios writes the scenario listing to w, in presentation order.
func writeScenarios(w io.Writer) {
	scenario.WriteListing(w, demo.Scenarios())
}
