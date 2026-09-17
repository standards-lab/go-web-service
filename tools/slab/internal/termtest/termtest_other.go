//go:build !linux

package termtest

import (
	"os"
	"testing"
)

// Terminal is one end of a pseudo-terminal pair. This platform allocates
// none, so Open never returns one.
type Terminal struct{}

// Open skips t: only Linux allocates the pair.
func Open(t *testing.T) *Terminal {
	t.Helper()
	t.Skip("termtest: pseudo-terminals are allocated on Linux only")
	return nil
}

// Writer is the terminal the code under test writes to.
func (term *Terminal) Writer() *os.File { return nil }

// Output returns everything written to the terminal.
func (term *Terminal) Output() string { return "" }
