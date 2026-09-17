package termtest

import (
	"fmt"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Terminal is one end of a pseudo-terminal pair: the slave, which the test
// hands to the code under test as its stdout, and the master, which reads
// back what was written to it.
type Terminal struct {
	master, slave *os.File
	read          chan string
}

// Open allocates a pseudo-terminal pair, or skips t when the system has none
// to give. The pair is closed when t ends.
func Open(t *testing.T) *Terminal {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("termtest: no pseudo-terminal available: %v", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		t.Skipf("termtest: unlocking the pseudo-terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		t.Skipf("termtest: naming the pseudo-terminal: %v", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Skipf("termtest: opening the pseudo-terminal's slave end: %v", err)
	}
	term := &Terminal{master: master, slave: slave, read: make(chan string, 1)}
	// The pair buffers only a few kilobytes between the ends, so the master
	// is drained as the code under test writes, not after.
	go func() {
		b, _ := io.ReadAll(master)
		term.read <- string(b)
	}()
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	return term
}

// Writer is the terminal the code under test writes to: an *os.File that
// is a terminal by every check.
func (term *Terminal) Writer() *os.File { return term.slave }

// Output closes the writing end and returns everything written to it. The
// line discipline turns each newline into a carriage return and newline,
// so a caller matches on content rather than the exact line ending.
func (term *Terminal) Output() string {
	_ = term.slave.Close()
	return <-term.read
}
