// Package configtest builds hermetically valid service configuration for
// tests. It is the single place the suites learn what the root config
// requires: when a subsystem's block gains a required field, it is set here
// once and every consuming test adapts.
//
// The package exports:
//
//   - [Minimal], an unfinalized root configuration with only the required
//     fields set, for tests that run Finalize themselves
//   - [Config], a finalized root configuration whose composition performs
//     no I/O
//   - [ClosedPort], a loopback port a connection attempt is refused on
package configtest
