package app

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/demo"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// mountDemo builds the demo mount over cfg: each scenario reads the
// environment cfg resolves and narrates through a reporter colored as cfg
// says. Both are read when the scenario runs, after cobra parses. The
// scenarios construct their own HTTP clients from that environment rather
// than through the infrastructure, since each one addresses the stack it
// needs (the service, Grafana, Tempo) by name.
func mountDemo(cfg *Config) *cobra.Command {
	return demo.Commands(cfg.runEnv, func(w io.Writer) *scenario.Reporter {
		return scenario.NewReporter(w, scenario.ColorEnabled(cfg.NoColor))
	})
}

// listCommand builds list, which prints the scenario listing.
func listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the scenarios and what each one needs running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			writeScenarios(cmd.OutOrStdout())
			return nil
		},
	}
}

// writeScenarios writes the scenario listing to w, in presentation order.
func writeScenarios(w io.Writer) {
	scenario.WriteListing(w, demo.Scenarios())
}
