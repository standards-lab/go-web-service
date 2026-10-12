package app

import (
	"context"
	"fmt"
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenarios"
)

// App is the program: its dependency graph and the [Nodes] that describe
// it, its command tree, the root flags every command accepts, and the
// streams it reads from and reports to.
type App struct {
	graph   *graph.Graph
	nodes   Nodes
	root    *cli.Command
	streams cli.Streams
}

// New describes the graph, layer by layer, lowest first, builds the command
// tree, and returns the App. It is cold: it constructs nothing, reads no
// configuration, and writes nothing until [App.Run]. Every run reads from
// and writes to streams; only a command that reads standard input reads
// Stdin.
func New(streams cli.Streams) *App {
	a := &App{
		graph: graph.New(),
		root: &cli.Command{
			Name:    "slab",
			Summary: "Call the service's endpoints and run its narrated scenarios",
			Footer: func(w io.Writer) {
				_, _ = fmt.Fprintln(w, "Scenarios:")
				writeScenarios(w)
			},
		},
		streams: streams,
	}
	flags := bindRootFlags(a.root.Flags())
	a.root.PreRun = flags.preRun
	defineInfrastructure(a.graph, &a.nodes, flags)
	defineScenarioNodes(a.graph, &a.nodes)
	a.root.Add(versionCommand())
	mountDomain(a.root, &a.nodes)
	mountAdmin(a.root, &a.nodes)
	a.root.Add(demoCommand(&a.nodes), listCommand())
	mountScenarios(a.root, &a.nodes)
	return a
}

// Run dispatches args, the program arguments without the program name, and
// returns the process exit code. The dispatcher builds the nodes the
// selected command's path declares with Use from the App's graph, with the
// lifecycle configuration node, and shuts what it built down before Run
// returns, so a Build, start, or shutdown error is reported with the
// command's result. An App runs one command at a time, since a graph.Graph
// is not safe for concurrent use.
func (a *App) Run(ctx context.Context, args []string) int {
	return cli.Run(ctx, a.root, args, a.streams, cli.WithGraph(a.graph, a.nodes.LifecycleConfig))
}

// Nodes is the App's graph, one handle per node: the single description of
// what the program is composed of. Each layer file's define function fills
// its own part of one Nodes value, and its constructors read the lower
// layers' nodes from that same value. Each field's node is named as the
// dispatcher labels its errors.
type Nodes struct {
	LifecycleConfig *graph.Node[lifecycle.Config]  // "lifecycle config"
	Base            *graph.Node[string]            // "base", the service's base URL
	Grafana         *graph.Node[string]            // "grafana", Grafana's base URL
	Tempo           *graph.Node[string]            // "tempo", Tempo's HTTP API base URL
	Repo            *graph.Node[string]            // "repo", the repository root override; empty when not given
	NoColor         *graph.Node[bool]              // "no color", print without ANSI color even on a terminal
	Client          *graph.Node[*httpx.Client]     // "client"
	Service         *graph.Node[scenarios.Service] // "service", the running service, checked ready at start
	GrafanaUp       *graph.Node[scenarios.Grafana] // "grafana up", the running Grafana, checked up at start
}

// Graph returns the graph a's commands are built from, as [New] described
// it. The program itself never calls it; it is published so a caller can,
// before the App runs, observe what a run builds with graph.Graph.Observe,
// or Replace a node's constructor with a substitute. A Replace changes the
// App itself, and the graph panics on a Replace once a run has built from
// it.
func (a *App) Graph() *graph.Graph { return a.graph }

// Nodes returns a handle on each of a's graph nodes, for a caller's Replace
// or for a command it adds over them.
func (a *App) Nodes() Nodes { return a.nodes }

// Root returns a's root command. A command a caller Adds to it before the
// App runs is part of the App's tree from then on.
func (a *App) Root() *cli.Command { return a.root }
