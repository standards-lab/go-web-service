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

	"github.com/standards-lab/go-web-service/tools/slab/events"
	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

const envPrefix = "SLAB"

const (
	defaultBase    = "http://127.0.0.1:8080"
	defaultGrafana = "http://127.0.0.1:3000"
)

const (
	defaultNATS       = "nats://127.0.0.1:4222"
	defaultNATSStream = "WEB_SERVICE"
	defaultNATSPrefix = "web-service"
)

func defineInfrastructure(g *graph.Graph, n *Nodes, flags *rootFlags) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Base = g.Define("base", newSetting(flags, "base", defaultBase, parseString))
	n.Grafana = g.Define("grafana", newSetting(flags, "grafana", defaultGrafana, parseString))
	n.Repo = g.Define("repo", newSetting(flags, "repo", "", parseString))
	n.NoColor = g.Define("no color", newSetting(flags, "no-color", false, strconv.ParseBool))
	n.NATS = g.Define("nats", newSetting(flags, "nats", defaultNATS, parseString))
	n.NATSStream = g.Define("nats stream", newSetting(flags, "nats-stream", defaultNATSStream, parseString))
	n.NATSPrefix = g.Define("nats prefix", newSetting(flags, "nats-prefix", defaultNATSPrefix, parseString))
	n.Client = g.Define("client", newClient(n.Base))
	n.Broker = g.Define("broker", newBroker(n.NATS, n.NATSStream, n.NATSPrefix))
}

func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

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

func parseString(s string) (string, error) { return s, nil }

type rootFlags struct {
	fs    *flag.FlagSet
	given map[string]bool
}

func bindRootFlags(fs *flag.FlagSet) *rootFlags {
	env := func(name string) string { return " (env " + config.EnvName(envPrefix, name) + ")" }
	fs.String("base", defaultBase, "the service's base URL"+env("base"))
	fs.String("grafana", defaultGrafana, "Grafana's base URL"+env("grafana"))
	fs.String("repo", "", "the repository root, when slab is not run from inside it"+env("repo"))
	fs.String("nats", defaultNATS, "the NATS server the service publishes its events to"+env("nats"))
	fs.String("nats-stream", defaultNATSStream, "the stream the service publishes its events to"+env("nats-stream"))
	fs.String("nats-prefix", defaultNATSPrefix, "the prefix of the service's event types"+env("nats-prefix"))
	fs.Bool("no-color", false, "print without ANSI color even on a terminal"+env("no-color"))
	return &rootFlags{fs: fs}
}

func (f *rootFlags) preRun(_ context.Context, inv *cli.Invocation) error {
	f.given = make(map[string]bool)
	f.fs.VisitAll(func(fl *flag.Flag) {
		f.given[fl.Name] = inv.Changed(fl.Name)
	})
	return nil
}

func (f *rootFlags) value(name string) (any, bool) {
	if !f.given[name] {
		return nil, false
	}
	return f.fs.Lookup(name).Value.(flag.Getter).Get(), true
}

func newClient(base *graph.Node[string]) func(*graph.Scope) (*httpx.Client, error) {
	return func(s *graph.Scope) (*httpx.Client, error) {
		return httpx.NewClient(s.Use(base)), nil
	}
}

func newBroker(url, stream, prefix *graph.Node[string]) func(*graph.Scope) (*events.Broker, error) {
	return func(s *graph.Scope) (*events.Broker, error) {
		return events.NewBroker(s.Use(url), s.Use(stream), s.Use(prefix))
	}
}

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
