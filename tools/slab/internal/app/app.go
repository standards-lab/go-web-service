package app

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// App is the application layer: it assembles the infrastructure, the domain
// layer, the admin layer, and the demo into a command tree, and runs the
// process.
type App struct {
	root   *cobra.Command
	stderr io.Writer
}

// New is the cold start: it composes the layers in dependency order and
// performs no I/O. The tree writes its output to stdout and its errors to
// stderr.
func New(stdout, stderr io.Writer) *App {
	cfg := &Config{}
	root := newRoot(cfg)
	root.SetOut(stdout)
	root.SetErr(stderr)

	infra := newInfrastructure(cfg)
	dom := newDomain(infra)
	adm := newAdmin(infra)

	root.AddCommand(commands(dom, adm, cfg)...)

	return &App{root: root, stderr: stderr}
}

// Run is the hot start: it executes the tree under ctx, which cancels every
// request in flight when the process is signalled. The tree silences cobra's
// own reporting, so the error a command returns is rendered here, through
// output.Error, and sets the exit code.
func (a *App) Run(ctx context.Context) int {
	if err := a.root.ExecuteContext(ctx); err != nil {
		output.Error(a.stderr, err)
		return 1
	}
	return 0
}

// newRoot builds the root command with cfg's persistent flags bound. Run
// without a subcommand, it prints its help and the scenario listing.
func newRoot(cfg *Config) *cobra.Command {
	root := &cobra.Command{
		Use:   "slab",
		Short: "Call the service's endpoints and run its narrated scenarios",
		Long: "slab calls the service's endpoints, one subcommand per endpoint under org and\n" +
			"admin, and runs its narrated scenarios: each one says what it is about to do,\n" +
			"does it against the running stack, and prints what it observed.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Help(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Scenarios:")
			writeScenarios(cmd.OutOrStdout())
			return nil
		},
	}
	cfg.bind(root.PersistentFlags())
	return root
}
