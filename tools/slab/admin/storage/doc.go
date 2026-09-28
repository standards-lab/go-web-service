// Package storage is slab's client-side counterpart of the service's
// admin/storage package: the storage command family, one subcommand per
// endpoint the object store's admin domain exposes under /admin/storage.
// Like its database sibling it administers nothing itself; it consumes the
// HTTP surface, and the one-to-one name across the two trees is for
// navigation.
//
// Neither endpoint takes a body, so the package has no entities.go.
// client.go names the route and sends one request per endpoint through
// httpx; commands.go builds the storage command and its two leaf
// subcommands, each rendering what came back through output.
package storage
