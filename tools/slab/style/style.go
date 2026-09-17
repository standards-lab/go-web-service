package style

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ColorEnabled reports whether output written to w should emit ANSI color:
// only when w is a terminal, NO_COLOR is unset, and the caller's own
// --no-color flag was not passed. It asks w rather than os.Stdout so the
// decision is about the stream actually written to, which a test can make a
// terminal without touching the process's own.
func ColorEnabled(w io.Writer, noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && isTerminal(w)
}

// isTerminal reports whether w is an open terminal: an *os.File whose
// descriptor answers as one. Any other writer (a buffer, a pipe wrapped in
// something else) is not.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Style is the one place ANSI escapes live. With On false every method
// returns its input unchanged, so the same code path serves a pipe and a
// terminal. A format's own file (sql.go, json.go, ...) builds its
// formatting entirely out of the primitives here; none of them reach past
// Style into another format's file.
type Style struct{ on bool }

// New returns a Style with color on or off.
func New(on bool) Style { return Style{on: on} }

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func (s Style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (s Style) Bold(text string) string    { return s.wrap(ansiBold, text) }
func (s Style) Dim(text string) string     { return s.wrap(ansiDim, text) }
func (s Style) Heading(text string) string { return s.wrap(ansiBold+ansiCyan, text) }
func (s Style) Caption(text string) string { return s.wrap(ansiMagenta, text) }
func (s Style) Key(text string) string     { return s.wrap(ansiBlue, text) }
func (s Style) Value(text string) string   { return s.wrap(ansiGreen, text) }
func (s Style) Status(text string) string  { return s.wrap(ansiBold+ansiYellow, text) }
