package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the scenarios and what each one needs running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			writeListing(cmd.OutOrStdout())
			return nil
		},
	}
}

// writeListing prints one line per registered scenario, name and summary,
// followed by one line per need with the mise task that satisfies it.
func writeListing(w io.Writer) {
	all := scenario.All()
	if len(all) == 0 {
		_, _ = fmt.Fprintln(w, "no scenarios registered")
		return
	}
	width := 0
	for _, s := range all {
		width = max(width, len(s.Name))
	}
	for _, s := range all {
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, s.Name, s.Summary)
		for _, n := range s.Needs {
			line := "needs " + n.What
			if n.Task != "" {
				line += " (mise run " + n.Task + ")"
			}
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, "", line)
		}
	}
}
