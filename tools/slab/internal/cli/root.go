// Package cli is slab's command tree: the root with its persistent flags,
// list, and one demo subcommand per registered scenario.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
)

// options are the root command's persistent flags after parsing.
type options struct {
	base    string
	grafana string
	tempo   string
	repo    string
	noColor bool
}

func (o *options) runEnv() env.Env {
	return env.Env{Base: o.base, Grafana: o.grafana, Tempo: o.tempo, Repo: o.repo}
}

// Root builds the command tree. Each call builds a fresh tree over the
// scenarios registered at that moment.
func Root() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:   "slab",
		Short: "Run the service's narrated scenarios",
		Long: "slab runs the service's narrated scenarios: each one says what it is about\n" +
			"to do, does it against the running stack, and prints what it observed.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Help(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Scenarios:")
			writeListing(cmd.OutOrStdout())
			return nil
		},
	}
	f := root.PersistentFlags()
	f.StringVar(&opts.base, "base", envOr("SLAB_BASE", "http://127.0.0.1:8080"), "the service's base URL (env SLAB_BASE)")
	f.StringVar(&opts.grafana, "grafana", envOr("SLAB_GRAFANA", "http://127.0.0.1:3000"), "Grafana's base URL (env SLAB_GRAFANA)")
	f.StringVar(&opts.tempo, "tempo", envOr("SLAB_TEMPO", "http://127.0.0.1:3200"), "Tempo's HTTP API base URL (env SLAB_TEMPO)")
	f.StringVar(&opts.repo, "repo", "", "the repository root, when slab is not run from inside it")
	f.BoolVar(&opts.noColor, "no-color", false, "print without ANSI color even on a terminal")

	root.AddCommand(listCommand(), demoCommand(opts))
	return root
}

// envOr returns the environment variable's value, or fallback when it is
// unset or empty.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
