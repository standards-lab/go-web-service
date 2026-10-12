package app

import (
	"context"
	"io"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/demo"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// demoCommand builds the demo command on the tree over the config node from
// n, the one node its scenarios read: each runs on slab's own scenario
// runner with the environment the config resolves, and narrates through a
// reporter over the invocation's stdout, colored when stdout is a terminal
// and neither NO_COLOR nor the config's NoColor says otherwise. The
// scenarios construct their own HTTP clients from that environment rather
// than through the client node, since each one addresses the stack it
// needs (the service, Grafana, Tempo) by name.
func demoCommand(n *Nodes) *cli.Command {
	config := n.Config
	return demo.Commands(scenario.Source{
		Node: config,
		Env: func(inv *cli.Invocation) env.Env {
			c := inv.Get(config)
			return env.Env{Base: c.Base, Grafana: c.Grafana, Tempo: c.Tempo, Repo: c.Repo}
		},
		Reporter: func(inv *cli.Invocation) *scenario.Reporter {
			return scenario.NewReporter(inv.Stdout, style.ColorEnabled(inv.Stdout, inv.Get(config).NoColor))
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
