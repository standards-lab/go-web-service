// Package organization is the organization domain layer: the recursive
// organization hierarchy — sibling-unique codes composing one unique path
// per node — served under /api/organizations as paginated, filtered, sorted
// reads and four commands.
//
// The package holds the whole layer in role-aggregated files, the layout
// standard every domain layer follows. entities.go declares the layer's
// data structures. database.go is the go-database translation: the
// projection, its recursive lineage CTE, directive lowering, and row
// scanning, and the command operations, exposed to the rest of the package
// only as complete operations — no statement, connection, or query type
// crosses out of it. service.go
// is the layer's one domain service, the direct map from endpoint to
// operation. handler.go is the layer's one handler, binding the service's
// methods to the route group the composition root mounts into the API
// module.
//
// The write side is four commands — create, edit, transfer, delete —
// returning identity and version only. Edit rewrites the descriptive
// fields; transfer moves the organization and owns the cycle check, run
// inside its transaction under the tree's advisory lock, since why a record
// changes determines its validation and handling. The guarded commands take
// their version precondition from If-Match: a missing header is 428, a
// stale version 412; state conflicts — a duplicate sibling code, a cycle, a
// delete with children — are 409.
//
// The path is projected at read time, never stored: the lineage CTE
// (standard SQL:1999, portable) walks the parent chain once per statement,
// so path is an ordinary projected field — filterable and sortable like any
// other — and the path lookup is a filter on it. The projection is the read
// contract's single field vocabulary: an unknown sort or filter name is a
// typed rejection before any SQL renders.
package organization
