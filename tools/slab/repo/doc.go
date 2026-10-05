// Package repo resolves the go-web-service repository root, the directory a
// scenario reads the service's own sources from.
//
// The package exports:
//
//   - [Module], the module path the root's go.mod declares
//   - [Root], which resolves the root for a run, and [FS], the root as a
//     filesystem
//   - [Find], which walks up from a directory to the root
//   - [ErrNotFound], the error when no directory on the walk is the root
package repo
