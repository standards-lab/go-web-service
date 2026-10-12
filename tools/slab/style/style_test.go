package style_test

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/style"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

// wrappers is every core wrapper by name, so a property of them all is
// checked on each.
var wrappers = map[string]func(style.Style, string) string{
	"Bold":    style.Style.Bold,
	"Dim":     style.Style.Dim,
	"Heading": style.Style.Heading,
	"Caption": style.Style.Caption,
	"Key":     style.Style.Key,
	"Value":   style.Style.Value,
	"Status":  style.Style.Status,
}

func TestStyle_OffLeavesTextUntouched(t *testing.T) {
	off := style.New(false)
	for name, wrap := range wrappers {
		if got := wrap(off, "x"); got != "x" {
			t.Errorf("%s with color off = %q", name, got)
		}
	}
}

func TestStyle_OnWrapsTextInEscapesAndKeepsIt(t *testing.T) {
	on := style.New(true)
	for name, wrap := range wrappers {
		got := wrap(on, "x")
		if got == "x" {
			t.Errorf("%s with color on left the text bare", name)
		}
		if strip(got) != "x" {
			t.Errorf("%s with color on changed the text under the escapes: %q", name, strip(got))
		}
		if got := wrap(on, ""); got != "" {
			t.Errorf("%s with color on wrapped empty text: %q", name, got)
		}
	}
}

func TestColorEnabled_IsOffForAWriterThatIsNoTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if style.ColorEnabled(&bytes.Buffer{}, false) {
		t.Error("ColorEnabled(buffer, false) = true, want false: a buffer is no terminal")
	}
}
