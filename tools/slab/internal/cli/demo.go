package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/internal/demo"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// demoCommand builds the demo subtree over the root's parsed flags: each
// scenario reads the environment they resolve and narrates through a
// reporter colored as they say.
func demoCommand(opts *options) *cobra.Command {
	return demo.Commands(opts.runEnv, func(w io.Writer) *scenario.Reporter {
		return scenario.NewReporter(w, scenario.ColorEnabled(opts.noColor))
	})
}
