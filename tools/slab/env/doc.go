// Package env holds the run configuration that a scenario's steps and Needs
// read from the context: the URLs slab targets and the repository root
// override. It imports no other slab package, so every layer, including
// httpx and repo below scenario, can read it without an import cycle.
package env
