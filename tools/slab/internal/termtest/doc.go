// Package termtest gives a test a real terminal to write to. Color is
// decided by whether the stream written to is a terminal, and under go test
// the process's stdout never is, so a test that asserts color reaches the
// output has to bring its own.
//
// The package exports:
//
//   - [Terminal], a pseudo-terminal pair
//   - [Open], which allocates one for a test
package termtest
