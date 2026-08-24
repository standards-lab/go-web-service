// Package process holds the parts of a binary's main sequence that run
// before the service's own infrastructure exists: reporting a failure or a
// usage error when there is no logger yet, the signal-derived root context,
// and the exit-code convention the reporters return. Both binaries —
// cmd/server and cmd/db — compose their run functions from it, so the
// convention cannot drift between them.
package process
