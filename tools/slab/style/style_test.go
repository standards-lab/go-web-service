package style

import (
	"regexp"
	"testing"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

func TestStyle_OffLeavesTextUntouched(t *testing.T) {
	off := New(false)
	if got := off.Bold("x"); got != "x" {
		t.Errorf("Bold with color off = %q", got)
	}
}
