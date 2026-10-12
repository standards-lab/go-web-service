package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// envPrefix is the prefix of every environment variable slab reads its
// configuration from: go-core's config convention composes the rest, such
// as SLAB_BASE and SLAB_SHUTDOWN_TIMEOUT.
const envPrefix = "SLAB"

// The defaults the URL settings read when neither their variable nor their
// root flag is given: the compose stack's local ports.
const (
	defaultBase    = "http://127.0.0.1:8080"
	defaultGrafana = "http://127.0.0.1:3000"
)

// defineInfrastructure defines the infrastructure nodes on g into n: the
// lifecycle configuration; one node per run setting, each named for its
// root flag and read from its default, its SLAB_* variable, and that flag
// given on the command line; and the HTTP client to the service, bound to
// the base node's URL. It constructs nothing: each value is read in its
// node's constructor, which runs only when a Build reaches the node, so a
// setting is read only for a command that declares it, or a node that
// depends on it, never in [New] and never for a run that builds nothing.
func defineInfrastructure(g *graph.Graph, n *Nodes, flags *rootFlags) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Base = g.Define("base", newSetting(flags, "base", defaultBase, parseString))
	n.Grafana = g.Define("grafana", newSetting(flags, "grafana", defaultGrafana, parseString))
	n.Repo = g.Define("repo", newSetting(flags, "repo", "", parseString))
	n.NoColor = g.Define("no color", newSetting(flags, "no-color", false, strconv.ParseBool))
	n.Client = g.Define("client", newClient(n.Base))
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

// newSetting returns the constructor of the run setting the root flag name
// overrides: it starts from def, replaces it with the variable composed
// from envPrefix and name, such as SLAB_BASE for "base" or SLAB_NO_COLOR
// for "no-color", when that is set and non-empty, parsing it with parse,
// and replaces that with the flag's value when the dispatch set the flag.
// An unparsable variable is an error naming it. It reads only when a Build
// reaches the node.
func newSetting[T any](flags *rootFlags, name string, def T, parse func(string) (T, error)) func(*graph.Scope) (T, error) {
	return func(*graph.Scope) (T, error) {
		v := def
		variable := config.EnvName(envPrefix, name)
		if s := os.Getenv(variable); s != "" {
			parsed, err := parse(s)
			if err != nil {
				var zero T
				return zero, fmt.Errorf("%s: %w", variable, err)
			}
			v = parsed
		}
		if given, ok := flags.value(name); ok {
			v = given.(T)
		}
		return v, nil
	}
}

// parseString is a string setting's parse: any value is the setting.
func parseString(s string) (string, error) { return s, nil }

// rootFlags are the root's flags, accepted at any depth: each overrides
// its setting's variable when it is given on the command line. Their
// defaults are the settings' own, so help shows them, and a flag left at
// its default overrides nothing, so an environment variable still applies.
type rootFlags struct {
	fs    *flag.FlagSet
	given map[string]bool // the flags the current dispatch set, from preRun
}

// bindRootFlags defines the root flags on fs, the root command's flag set,
// each naming the variable it overrides.
func bindRootFlags(fs *flag.FlagSet) *rootFlags {
	env := func(name string) string { return " (env " + config.EnvName(envPrefix, name) + ")" }
	fs.String("base", defaultBase, "the service's base URL"+env("base"))
	fs.String("grafana", defaultGrafana, "Grafana's base URL"+env("grafana"))
	fs.String("repo", "", "the repository root, when slab is not run from inside it"+env("repo"))
	fs.Bool("no-color", false, "print without ANSI color even on a terminal"+env("no-color"))
	return &rootFlags{fs: fs}
}

// preRun is the root's PreRun: it records which root flags the dispatch
// set, wherever on the command line, before the Build reads them. A flag
// variable alone cannot say so, since a flag given its default value still
// overrides the environment.
func (f *rootFlags) preRun(_ context.Context, inv *cli.Invocation) error {
	f.given = make(map[string]bool)
	f.fs.VisitAll(func(fl *flag.Flag) {
		f.given[fl.Name] = inv.Changed(fl.Name)
	})
	return nil
}

// value returns the root flag name's value, and whether the dispatch set
// it.
func (f *rootFlags) value(name string) (any, bool) {
	if !f.given[name] {
		return nil, false
	}
	return f.fs.Lookup(name).Value.(flag.Getter).Get(), true
}

// newClient returns the client node's constructor: the HTTP client to the
// service, bound to the URL the base node resolves.
func newClient(base *graph.Node[string]) func(*graph.Scope) (*httpx.Client, error) {
	return func(s *graph.Scope) (*httpx.Client, error) {
		return httpx.NewClient(s.Use(base)), nil
	}
}

// outputSource returns the output.Source a command family on the tree
// renders through: the no color node, which the family declares with Use,
// and the Output over the invocation's stdout, colored when stdout is a
// terminal and neither NO_COLOR nor the no color node, the SLAB_NO_COLOR
// variable or --no-color, says otherwise.
func outputSource(n *Nodes) output.Source {
	node := n.NoColor
	return output.Source{
		Node: node,
		New: func(inv *cli.Invocation) *output.Output {
			noColor := inv.Get(node)
			return output.New(inv.Stdout, func() bool {
				return style.ColorEnabled(inv.Stdout, noColor)
			})
		},
	}
}
