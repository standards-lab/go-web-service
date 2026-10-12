package app

import (
	"context"
	"flag"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// legacy is the temporary bridge to the command families still on cobra:
// docs, admin, demo, and list. Each moves onto the go-cli-sdk tree in
// a later slice of the commands task, and this file goes with the last of
// them. Until then a run whose subcommand is one of them is dispatched to
// a cobra tree built as before the move, with its own flags, words,
// requests, and exit codes, so nothing about those commands changes.
type legacy struct {
	streams cli.Streams
	names   []string // the top-level words the cobra tree serves
}

// mountLegacy lists each command family still on cobra under root, by its
// cobra name and Short, so the root's help shows them; the entries are
// placeholders the dispatch never reaches, since [App.Run] hands any run
// that selects one to the cobra tree first. It reads no configuration: the
// tree it builds to read the names binds no flags.
func mountLegacy(root *cli.Command, streams cli.Streams) *legacy {
	l := &legacy{streams: streams}
	tree, _ := l.tree(&legacyConfig{})
	for _, c := range tree.Commands() {
		l.names = append(l.names, c.Name())
		root.Add(&cli.Command{Name: c.Name(), Summary: c.Short})
	}
	return l
}

// selects reports whether args select a command family still on cobra: it
// finds the subcommand word the way the root's dispatch would, skipping
// the root flags in rootFlags and the value each non-boolean one takes, and
// checks it against the families the cobra tree serves.
func (l *legacy) selects(rootFlags *flag.FlagSet, args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		if len(arg) < 2 || arg[0] != '-' {
			return slices.Contains(l.names, arg)
		}
		name := strings.TrimPrefix(arg[1:], "-")
		if strings.Contains(name, "=") {
			continue
		}
		if f := rootFlags.Lookup(name); f != nil && !isBoolFlag(f) {
			i++
		}
	}
	return false
}

// isBoolFlag reports whether f is a boolean flag, which takes no separate
// value argument.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// run builds the cobra tree afresh, binds its persistent flags, executes
// args over it under ctx, and returns the exit code. The tree silences
// cobra's own reporting, so the error a command returns is rendered here,
// through the output's Error, and exits 1, as it did before the move.
func (l *legacy) run(ctx context.Context, args []string) int {
	cfg := &legacyConfig{}
	root, out := l.tree(cfg)
	cfg.bind(root.PersistentFlags())
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		out.Error(err)
		return 1
	}
	return 0
}

// tree builds the cobra root over cfg, with the command families mounted,
// and returns it with the output its commands render through, over the
// streams. Building it reads nothing: cfg is populated when cobra parses,
// after run binds the flags.
func (l *legacy) tree(cfg *legacyConfig) (*cobra.Command, *output.Output) {
	root := &cobra.Command{
		Use:           "slab",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(l.streams.Stdin)
	root.SetOut(l.streams.Stdout)
	root.SetErr(l.streams.Stderr)
	out := newOutput(l.streams.Stdout, l.streams.Stderr, cfg)
	infra := newInfrastructure(cfg)
	root.AddCommand(commands(newDomain(infra), newAdmin(infra), out, cfg)...)
	return root, out
}

// legacyConfig is the cobra tree's persistent flags, resolved once cobra
// parses them during execution. The tree is built before that, so a layer
// that depends on a value reads it through a closure when a subcommand
// runs, never at construction.
type legacyConfig struct {
	Base    string
	Grafana string
	Tempo   string
	Repo    string
	NoColor bool
}

// bind registers the persistent flags on f, each URL defaulting to its
// SLAB_* variable when that is set and non-empty.
func (c *legacyConfig) bind(f *pflag.FlagSet) {
	f.StringVar(&c.Base, "base", envOr("SLAB_BASE", defaultBase), "the service's base URL (env SLAB_BASE)")
	f.StringVar(&c.Grafana, "grafana", envOr("SLAB_GRAFANA", defaultGrafana), "Grafana's base URL (env SLAB_GRAFANA)")
	f.StringVar(&c.Tempo, "tempo", envOr("SLAB_TEMPO", defaultTempo), "Tempo's HTTP API base URL (env SLAB_TEMPO)")
	f.StringVar(&c.Repo, "repo", "", "the repository root, when slab is not run from inside it")
	f.BoolVar(&c.NoColor, "no-color", false, "print without ANSI color even on a terminal")
}

// runEnv is the environment the scenarios resolve, from the flags as parsed.
func (c *legacyConfig) runEnv() env.Env {
	return env.Env{Base: c.Base, Grafana: c.Grafana, Tempo: c.Tempo, Repo: c.Repo}
}

// envOr returns the environment variable's value, or fallback when it is
// unset or empty.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
