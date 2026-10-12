package app

import (
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// defineInfrastructure defines the infrastructure nodes on g into n: the
// configurations, read from SLAB_* variables, and, for config, the root
// flags given on the command line; and the HTTP client to the service,
// bound to config's base URL. It constructs nothing: each value is read in
// its node's constructor, which runs only when a Build reaches the node,
// so configuration is read only for a command that declares a node, never
// in [New] and never for a run that builds nothing.
func defineInfrastructure(g *graph.Graph, n *Nodes, flags *rootFlags) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Config = g.Define("config", newConfig(flags))
	n.Client = g.Define("client", newClient(n.Config))
}

// newClient returns the client node's constructor: the HTTP client to the
// service, bound to the base URL config resolves. It is the one place a
// command on the tree gets an httpx.Client from; the demo scenarios
// construct their own, as each addresses the stack it needs by name.
func newClient(config *graph.Node[Config]) func(*graph.Scope) (*httpx.Client, error) {
	return func(s *graph.Scope) (*httpx.Client, error) {
		return httpx.NewClient(s.Use(config).Base), nil
	}
}

// outputSource returns the output.Source a command family on the tree
// renders through: the config node, which the family declares with Use,
// and the Output over the invocation's streams, colored when stdout is a
// terminal and neither NO_COLOR nor the config's NoColor, the SLAB_NO_COLOR
// variable or --no-color, says otherwise.
func outputSource(n *Nodes) output.Source {
	config := n.Config
	return output.Source{
		Node: config,
		New: func(inv *cli.Invocation) *output.Output {
			noColor := inv.Get(config).NoColor
			return output.New(inv.Stdout, inv.Stderr, func() bool {
				return style.ColorEnabled(inv.Stdout, noColor)
			})
		},
	}
}

// newLifecycleConfig reads the lifecycle configuration, the shutdown
// budget, from its default and SLAB_SHUTDOWN_TIMEOUT, and validates it. The
// dispatcher adds this node to every Build, so a command that declares any
// node reads it.
func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

// Infrastructure holds what the cobra families are composed on: the HTTP client to
// the service. The client binds to the base URL, and that is a persistent
// flag cobra parses during execution, after the tree is built, so the struct
// holds the resolved config and constructs the client on demand through
// Client. The struct stops at the composition root: the layer files close
// their own client constructors over it, and a package receives the
// constructor as a parameter, never the struct itself.
type Infrastructure struct {
	cfg *legacyConfig
}

// newInfrastructure constructs the infrastructure over cfg. Construction
// opens nothing and reads no flag: cfg is populated later, when cobra parses.
func newInfrastructure(cfg *legacyConfig) *Infrastructure {
	return &Infrastructure{cfg: cfg}
}

// Client constructs the HTTP client over the base URL as parsed. It is the
// one place outside the demo scenarios that calls httpx.NewClient, and it is
// called from a subcommand's RunE, never at construction.
func (i *Infrastructure) Client() *httpx.Client {
	return httpx.NewClient(i.cfg.Base)
}

// newOutput constructs the output every direct command renders through:
// results to stdout, failures to stderr, colored when stdout is a terminal
// and neither NO_COLOR nor --no-color says otherwise. The streams are
// bound here; the color decision is a closure over cfg that the output
// asks when a command renders, since --no-color is a persistent flag cobra
// parses during execution, after this runs. Reading cfg.NoColor here would
// read it before it is set.
func newOutput(stdout, stderr io.Writer, cfg *legacyConfig) *output.Output {
	return output.New(stdout, stderr, func() bool {
		return style.ColorEnabled(stdout, cfg.NoColor)
	})
}
