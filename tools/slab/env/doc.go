// Package env is the run configuration a scenario's steps and Needs read
// from context: the URLs slab was pointed at, and the repository root
// override. It has no dependency on any other slab package, so every layer
// — including httpx and repo, below scenario — can read it without a cycle.
package env
