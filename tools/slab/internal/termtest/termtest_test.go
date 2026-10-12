package termtest_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/termtest"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// The terminal Open gives reads as one to style.ColorEnabled, which then
// yields to the flag and NO_COLOR. The test lives here, not in style,
// because a root-level package's tests import nothing under internal/.
func TestColorEnabled_OnATerminalYieldsToTheFlagAndNO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	tty := termtest.Open(t).Writer()
	if !style.ColorEnabled(tty, false) {
		t.Error("ColorEnabled(terminal, false) = false, want true")
	}
	if style.ColorEnabled(tty, true) {
		t.Error("ColorEnabled(terminal, true) = true, want false: --no-color wins")
	}
	t.Setenv("NO_COLOR", "1")
	if style.ColorEnabled(tty, false) {
		t.Error("ColorEnabled(terminal, false) with NO_COLOR set = true, want false")
	}
}
