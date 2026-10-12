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
)

// envPrefix is the prefix of every environment variable slab reads its
// configuration from: go-core's config convention composes the rest, such
// as SLAB_BASE and SLAB_SHUTDOWN_TIMEOUT.
const envPrefix = "SLAB"

// The defaults a [Config] finalizes to: the compose stack's local ports.
const (
	defaultBase    = "http://127.0.0.1:8080"
	defaultGrafana = "http://127.0.0.1:3000"
	defaultTempo   = "http://127.0.0.1:3200"
)

// Config is slab's run configuration: the URLs it targets, the repository
// root override, and whether to print without color. It is the value of
// the config node, read when a command that declares the node runs: its
// defaults, then the SLAB_* variables, then the root flags given on the
// command line. Env records the variable names Finalize read.
type Config struct {
	Base    string // the service's base URL
	Grafana string // Grafana's base URL
	Tempo   string // Tempo's HTTP API base URL
	Repo    string // the repository root override; empty when not given
	NoColor bool   // print without ANSI color even on a terminal
	Env     ConfigEnv
}

// ConfigEnv names the environment variables [Config.Finalize] reads, one
// per field. An empty name disables that override.
type ConfigEnv struct {
	Base, Grafana, Tempo, Repo, NoColor string
}

// NewConfigEnv composes each variable name from prefix with
// [config.EnvName], such as SLAB_BASE for "SLAB"; an empty prefix returns
// the zero ConfigEnv.
func NewConfigEnv(prefix string) ConfigEnv {
	return ConfigEnv{
		Base:    config.EnvName(prefix, "base"),
		Grafana: config.EnvName(prefix, "grafana"),
		Tempo:   config.EnvName(prefix, "tempo"),
		Repo:    config.EnvName(prefix, "repo"),
		NoColor: config.EnvName(prefix, "no", "color"),
	}
}

// Finalize applies the defaults to the URLs left empty, then reads the
// envPrefix-named overrides: a set, non-empty variable replaces its field.
// An unparsable no-color override is an error naming the variable.
func (c *Config) Finalize(envPrefix string) error {
	c.Env = NewConfigEnv(envPrefix)
	for _, s := range []struct {
		field    *string
		fallback string
		name     string
	}{
		{&c.Base, defaultBase, c.Env.Base},
		{&c.Grafana, defaultGrafana, c.Env.Grafana},
		{&c.Tempo, defaultTempo, c.Env.Tempo},
		{&c.Repo, "", c.Env.Repo},
	} {
		if *s.field == "" {
			*s.field = s.fallback
		}
		if v := lookupEnv(s.name); v != "" {
			*s.field = v
		}
	}
	if v := lookupEnv(c.Env.NoColor); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Env.NoColor, err)
		}
		c.NoColor = b
	}
	return nil
}

// lookupEnv returns the variable's value, or "" for an empty name, which
// composes no override.
func lookupEnv(name string) string {
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}

// rootFlags are the root's flags, accepted at any depth: each overrides
// its [Config] field, over the environment, when it is given on the command
// line. Their defaults are the Config's own, so help shows them, and a flag
// left at its default overrides nothing, so an environment variable still
// applies.
type rootFlags struct {
	fs      *flag.FlagSet
	base    string
	grafana string
	tempo   string
	repo    string
	noColor bool
	given   map[string]bool // the flags the current dispatch set, from preRun
}

// bindRootFlags defines the root flags on fs, the root command's flag set.
func bindRootFlags(fs *flag.FlagSet) *rootFlags {
	names := NewConfigEnv(envPrefix)
	f := &rootFlags{fs: fs}
	fs.StringVar(&f.base, "base", defaultBase, "the service's base URL (env "+names.Base+")")
	fs.StringVar(&f.grafana, "grafana", defaultGrafana, "Grafana's base URL (env "+names.Grafana+")")
	fs.StringVar(&f.tempo, "tempo", defaultTempo, "Tempo's HTTP API base URL (env "+names.Tempo+")")
	fs.StringVar(&f.repo, "repo", "", "the repository root, when slab is not run from inside it (env "+names.Repo+")")
	fs.BoolVar(&f.noColor, "no-color", false, "print without ANSI color even on a terminal (env "+names.NoColor+")")
	return f
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

// apply overrides c's fields with the root flags the dispatch set.
func (f *rootFlags) apply(c *Config) {
	for name, set := range map[string]func(){
		"base":     func() { c.Base = f.base },
		"grafana":  func() { c.Grafana = f.grafana },
		"tempo":    func() { c.Tempo = f.tempo },
		"repo":     func() { c.Repo = f.repo },
		"no-color": func() { c.NoColor = f.noColor },
	} {
		if f.given[name] {
			set()
		}
	}
}

// newConfig returns the config node's constructor: it finalizes a Config
// from the defaults and the SLAB_* variables, then applies the root flags
// given on the command line, so a flag overrides its variable. It reads
// only when a Build reaches the node.
func newConfig(flags *rootFlags) func(*graph.Scope) (Config, error) {
	return func(*graph.Scope) (Config, error) {
		var c Config
		if err := c.Finalize(envPrefix); err != nil {
			return Config{}, err
		}
		flags.apply(&c)
		return c, nil
	}
}
