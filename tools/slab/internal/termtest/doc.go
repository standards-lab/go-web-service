// Package termtest gives a test a real terminal to write to. Color is
// decided by whether the stream written to is a terminal, and under go test
// the process's stdout never is, so a test that asserts color reaches the
// output has to bring its own: Open allocates a pseudo-terminal pair and
// hands the test its slave end, an *os.File any terminal check answers yes
// to, and reads back what was written to it through the master end.
//
// Only Linux allocates the pair; elsewhere Open skips the test.
package termtest
