package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Render writes one block of a scenario's narration, the form go-cli-sdk's
// scenario.Reporter.Show takes: Show indents what it writes past the prose
// and prints nothing when it fails. A block carries no caption; a step
// prints the caption as a plain Note before the block, so only the block
// itself is ever colored. Each constructor below takes the style the run
// decided on, so the block is colored only when the run's color is on.
type Render = func(io.Writer) error

// SQL returns the Render of an SQL statement as authored, its keywords in
// bold under st, with any trailing newlines dropped so the block ends at
// its last line.
func SQL(st style.Style, text string) Render {
	return func(w io.Writer) error {
		_, err := fmt.Fprintln(w, st.SQL(strings.TrimRight(text, "\n")))
		return err
	}
}

// Table returns the Render of name/value pairs, one per line, the names
// keyed under st and padded so the values line up two spaces past the
// longest name.
func Table(st style.Style, rows [][2]string) Render {
	return func(w io.Writer) error {
		width := 0
		for _, row := range rows {
			width = max(width, len(row[0]))
		}
		for _, row := range rows {
			pad := strings.Repeat(" ", width-len(row[0]))
			if _, err := fmt.Fprintf(w, "%s%s  %s\n", st.Key(row[0]), pad, row[1]); err != nil {
				return err
			}
		}
		return nil
	}
}
