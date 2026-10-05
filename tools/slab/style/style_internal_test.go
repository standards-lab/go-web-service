package style

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/termtest"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

func TestStyle_OffLeavesTextUntouched(t *testing.T) {
	off := New(false)
	if got := off.Bold("x"); got != "x" {
		t.Errorf("Bold with color off = %q", got)
	}
}

func TestColorEnabled_IsOffForAWriterThatIsNoTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if ColorEnabled(&bytes.Buffer{}, false) {
		t.Error("ColorEnabled(buffer, false) = true, want false: a buffer is no terminal")
	}
}

func TestColorEnabled_OnATerminalYieldsToTheFlagAndNO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	tty := termtest.Open(t).Writer()
	if !ColorEnabled(tty, false) {
		t.Error("ColorEnabled(terminal, false) = false, want true")
	}
	if ColorEnabled(tty, true) {
		t.Error("ColorEnabled(terminal, true) = true, want false: --no-color wins")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(tty, false) {
		t.Error("ColorEnabled(terminal, false) with NO_COLOR set = true, want false")
	}
}
