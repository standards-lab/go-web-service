package scenario_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Note wraps prose at 80 columns, the two-column indent included, so 78
// columns of text fit on a line.
func TestReporter_NoteWrapsProseAtWordBoundaries(t *testing.T) {
	url := "http://localhost:3200/api/traces/" + strings.Repeat("4bf92f3577b34da6a3ce929d0e0e4736", 3)
	for name, tc := range map[string]struct {
		text string
		want string
	}{
		"short text stays on one line": {
			text: "a short line",
			want: "  a short line\n",
		},
		"a break falls at the last word that fits": {
			// 15 words of "word" are 74 columns; a sixteenth would make 79.
			text: strings.Repeat("word ", 20),
			want: "  " + strings.TrimSpace(strings.Repeat("word ", 15)) + "\n" +
				"  " + strings.TrimSpace(strings.Repeat("word ", 5)) + "\n",
		},
		"an unbreakable token stands alone, past the width": {
			text: "polls " + url + " until it answers",
			want: "  polls\n  " + url + "\n  until it answers\n",
		},
		"a newline is a hard break and an empty line stays": {
			text: "first sentence here.\nsecond sentence.\n\nthird.",
			want: "  first sentence here.\n  second sentence.\n\n  third.\n",
		},
		"runs of whitespace collapse": {
			text: "  two   words  ",
			want: "  two words\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			scenario.NewReporter(&out, false).Note("%s", tc.text)
			if out.String() != tc.want {
				t.Errorf("Note printed:\n%q\nwant:\n%q", out.String(), tc.want)
			}
		})
	}
}

func TestReporter_NoteFormatsItsArgumentsBeforeWrapping(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.Note("%d rows in %q", 7, "default")
	if want := "  7 rows in \"default\"\n"; out.String() != want {
		t.Errorf("Note printed %q, want %q", out.String(), want)
	}
}
