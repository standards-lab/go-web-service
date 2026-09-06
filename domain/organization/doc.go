// Package organization is the organization domain layer: the recursive
// organization hierarchy, sibling-unique codes composing one unique path
// per node, served under /api/organizations as paginated, filtered, sorted
// reads and four commands.
//
// The layer's SQL is the statements/ directory, one authored file per
// statement with its tier header; database.go is the domain's SQL client,
// the sole importer of the query library: it compiles the directory once
// against the service's pattern catalog, registers the inventory, binds
// each statement to a typed handle, and exposes the operations as the
// store's methods. No statement, session, or query type crosses out of it.
// entities.go owns the shapes and their rules: each command validates
// itself, and the entities' tags are the binding and scan contract.
// service.go is the direct map from endpoint to operation; handler.go
// binds the service to the route group the composition root mounts into
// the API module.
//
// The write side is four commands returning identity and version only.
// Edit is full replacement of the descriptive fields, so it is the PUT of
// the resource; transfer is an action, a named transition with its own
// protocol, so it is a POST on its own path. Transfer owns the cycle
// check, run inside its transaction under the tree's advisory lock, taken
// through the data package by its registered name. The
// guarded commands take their version precondition from If-Match: a
// missing header is 428, a stale version 412; state conflicts are 409.
// The path is projected at read time by the lineage CTE (standard
// SQL:1999) and is an ordinary contract field, filterable and sortable
// like any other.
package organization
