package scenario

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestWrap_LeavesShortTextOnOneLine(t *testing.T) {
	got := wrap("a short line", 80)
	if want := []string{"a short line"}; !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrap_BreaksAtAWordBoundaryWithinTheWidth(t *testing.T) {
	got := wrap("the quick brown fox jumps over the lazy dog", 15)
	want := []string{"the quick brown", "fox jumps over", "the lazy dog"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
	for _, line := range got {
		if len(line) > 15 {
			t.Errorf("line %q is %d columns, over 15", line, len(line))
		}
	}
}

func TestWrap_LeavesAnUnbreakableTokenIntactOnItsOwnLine(t *testing.T) {
	url := "http://localhost:3200/api/traces/4bf92f3577b34da6a3ce929d0e0e4736"
	got := wrap("polls "+url+" until it answers", 20)
	want := []string{"polls", url, "until it answers"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrap_HonorsNewlinesAsHardBreaksAndKeepsEmptyLines(t *testing.T) {
	got := wrap("first sentence here.\nsecond sentence, longer than the width.\n\nthird.", 22)
	want := []string{
		"first sentence here.",
		"second sentence,",
		"longer than the width.",
		"",
		"third.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrap_CollapsesRunsOfWhitespace(t *testing.T) {
	got := wrap("  two   words  ", 80)
	if want := []string{"two words"}; !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestReporter_NoteWrapsAtEightyColumnsWithTheIndentOnEveryLine(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Note("%s", strings.Repeat("word ", 40))
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("Note printed %d lines, want several:\n%s", len(lines), out.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, indent+"word") {
			t.Errorf("line %q does not start with the indent", line)
		}
		if len(line) > columns {
			t.Errorf("line %q is %d columns, over %d", line, len(line), columns)
		}
	}
	// The 78 columns past the indent hold 15 words: 15 of "word" joined by
	// spaces are 74 wide, and a sixteenth would make 79.
	if want := indent + strings.TrimSpace(strings.Repeat("word ", 15)); lines[0] != want {
		t.Errorf("first line = %q, want %q", lines[0], want)
	}
}

func TestReporter_NoteFormatsItsArgumentsBeforeWrapping(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Note("%d rows in %q", 7, "default")
	if want := "  7 rows in \"default\"\n"; out.String() != want {
		t.Errorf("Note printed %q, want %q", out.String(), want)
	}
}

func TestReporter_JSONPrintsACaptionedColoredBlockAsAuthored(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.JSON("data/seeds/default.json", []byte("{\"organizations\": [{\"code\": \"acme\"}]}\n"))
	want := "\n" +
		"  data/seeds/default.json\n" +
		"    {\"organizations\": [{\"code\": \"acme\"}]}\n" +
		"\n"
	if out.String() != want {
		t.Errorf("JSON printed:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	on := NewReporter(&out, true)
	on.JSON("caption", []byte(`{"code": "acme"}`))
	got := out.String()
	if !strings.Contains(got, ansiMagenta+"caption"+ansiReset) {
		t.Error("the caption is not styled as a caption")
	}
	if !strings.Contains(got, ansiBlue+`"code"`+ansiReset+": "+ansiGreen+`"acme"`+ansiReset) {
		t.Errorf("the body is not colored as JSON:\n%q", got)
	}
}

func TestReporter_JSONPrintsANonJSONDocumentAsIs(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.JSON("a file", []byte("not json"))
	if want := "\n  a file\n    not json\n\n"; out.String() != want {
		t.Errorf("JSON printed %q, want %q", out.String(), want)
	}
}
