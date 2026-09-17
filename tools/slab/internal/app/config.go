package app

import (
	"os"

	"github.com/spf13/pflag"

	"github.com/standards-lab/go-web-service/tools/slab/env"
)

// Config is the root command's persistent flags, resolved once cobra parses
// them during execution. The tree is built before that, so a layer that
// depends on a value reads it through a closure when a subcommand runs,
// never at construction.
type Config struct {
	Base    string
	Grafana string
	Tempo   string
	Repo    string
	NoColor bool
}

// bind registers the persistent flags on f, each URL defaulting to its
// SLAB_* variable when that is set and non-empty.
func (c *Config) bind(f *pflag.FlagSet) {
	f.StringVar(&c.Base, "base", envOr("SLAB_BASE", "http://127.0.0.1:8080"), "the service's base URL (env SLAB_BASE)")
	f.StringVar(&c.Grafana, "grafana", envOr("SLAB_GRAFANA", "http://127.0.0.1:3000"), "Grafana's base URL (env SLAB_GRAFANA)")
	f.StringVar(&c.Tempo, "tempo", envOr("SLAB_TEMPO", "http://127.0.0.1:3200"), "Tempo's HTTP API base URL (env SLAB_TEMPO)")
	f.StringVar(&c.Repo, "repo", "", "the repository root, when slab is not run from inside it")
	f.BoolVar(&c.NoColor, "no-color", false, "print without ANSI color even on a terminal")
}

// runEnv is the environment the scenarios resolve, from the flags as parsed.
func (c *Config) runEnv() env.Env {
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
