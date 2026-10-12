package style_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// The escapes SQL emits, spelled out so each case asserts the exact bytes
// a terminal receives rather than restating SQL through Style's methods.
const (
	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	green = "\x1b[32m"
	reset = "\x1b[0m"
)

// Color off leaves every case below untouched: comments, markers, and
// keywords alike.
func TestSQL_OffLeavesTextUntouched(t *testing.T) {
	off := style.New(false)
	sql := "--| tier: native\nINSERT INTO t (a) -- note\nVALUES ({{code}})\n{{> app.identity}}"
	if got := off.SQL(sql); got != sql {
		t.Errorf("SQL with color off = %q", got)
	}
}

// settings holds SET and order holds ORDER, so a keyword matched inside a
// longer word would show here; lower-case select is no keyword, since the
// match is exact.
func TestSQL_BoldsWholeWordKeywordsOnly(t *testing.T) {
	got := style.New(true).SQL("SELECT settings, order_no FROM t ORDER BY id, select")
	want := bold + "SELECT" + reset + " settings, order_no " +
		bold + "FROM" + reset + " t " +
		bold + "ORDER" + reset + " " + bold + "BY" + reset + " id, select"
	if got != want {
		t.Errorf("SQL = %q\nwant %q", got, want)
	}
}

func TestSQL(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		// The keywords RETURNING and INTO in the annotation stay unbolded:
		// the line is dimmed whole, indentation included.
		"an annotation line is dimmed whole": {
			"  --| native: postgres — RETURNING INTO (Oracle)",
			dim + "  --| native: postgres — RETURNING INTO (Oracle)" + reset,
		},
		"a trailing comment is dimmed from its dashes": {
			"RETURNING id -- SELECT is not bold here",
			bold + "RETURNING" + reset + " id " + dim + "-- SELECT is not bold here" + reset,
		},
		"markers take the value color, braces included": {
			"VALUES ({{parent_id:uuid}}, {{code}}, {{name}})",
			bold + "VALUES" + reset + " (" + green + "{{parent_id:uuid}}" + reset + ", " +
				green + "{{code}}" + reset + ", " + green + "{{name}}" + reset + ")",
		},
		"a pattern include on its own line is a marker": {
			"{{> app.identity}}",
			green + "{{> app.identity}}" + reset,
		},
		"a marker inside a comment stays dim only": {
			"-- create ends with {{> app.identity}} and reads the same columns.",
			dim + "-- create ends with {{> app.identity}} and reads the same columns." + reset,
		},
		"a marker inside a trailing comment stays dim only": {
			"SELECT 1 -- see {{code}}",
			bold + "SELECT" + reset + " 1 " + dim + "-- see {{code}}" + reset,
		},
		"each line is colored on its own": {
			"--| tier: native\nINSERT INTO t\n{{> app.identity}}",
			dim + "--| tier: native" + reset + "\n" +
				bold + "INSERT" + reset + " " + bold + "INTO" + reset + " t\n" +
				green + "{{> app.identity}}" + reset,
		},
		"an unclosed marker is code": {
			"SELECT {{code FROM t",
			bold + "SELECT" + reset + " {{code " + bold + "FROM" + reset + " t",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := style.New(true).SQL(tc.in); got != tc.want {
				t.Errorf("SQL(%q) = %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}
