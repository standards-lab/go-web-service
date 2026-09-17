package scenario

import (
	"fmt"
	"io"
)

// WriteListing prints one line per scenario in all, name and summary,
// followed by one line per need with the mise task that satisfies it.
func WriteListing(w io.Writer, all []Scenario) {
	if len(all) == 0 {
		_, _ = fmt.Fprintln(w, "no scenarios")
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
