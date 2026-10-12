package output_test

import (
	"bytes"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// render runs r over a fresh buffer and returns what it wrote.
func render(t *testing.T, r output.Render) string {
	t.Helper()
	var buf bytes.Buffer
	if err := r(&buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSQL_WritesTheStatementAsAuthoredEndingAtItsLastLine(t *testing.T) {
	got := render(t, output.SQL(style.New(false), "SELECT id\nFROM t\n\n"))
	if want := "SELECT id\nFROM t\n"; got != want {
		t.Errorf("SQL wrote %q, want %q", got, want)
	}
}

func TestSQL_BoldsTheKeywordsWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.SQL(on, "SELECT id FROM t"))
	if want := on.SQL("SELECT id FROM t") + "\n"; got != want {
		t.Errorf("SQL wrote %q, want %q", got, want)
	}
}

func TestTable_AlignsTheValuesPastTheLongestName(t *testing.T) {
	got := render(t, output.Table(style.New(false), [][2]string{{"$1", "parent_id"}, {"$10", "code"}}))
	if want := "$1   parent_id\n$10  code\n"; got != want {
		t.Errorf("Table wrote %q, want %q", got, want)
	}
}

func TestTable_KeysTheNamesWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.Table(on, [][2]string{{"$1", "parent_id"}}))
	if want := on.Key("$1") + "  parent_id\n"; got != want {
		t.Errorf("Table wrote %q, want %q", got, want)
	}
}
