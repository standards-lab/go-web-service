// Package organization is the organization domain layer: the recursive
// organization hierarchy — sibling-unique codes composing one unique path
// per node — served as paginated, filtered, sorted reads under
// /api/organizations.
//
// The package holds the whole layer in role-aggregated files, the layout
// standard every domain layer follows. entities.go declares the layer's
// data structures. database.go is the go-database translation: the
// projection, its recursive lineage CTE, directive lowering, and row
// scanning, exposed to the rest of the package only as complete operations
// — no statement, connection, or query type crosses out of it. service.go
// is the layer's one domain service, the direct map from endpoint to
// operation. handler.go is the layer's one handler, binding the service's
// methods to the route group the composition root mounts into the API
// module.
//
// The path is projected at read time, never stored: the lineage CTE
// (standard SQL:1999, portable) walks the parent chain once per statement,
// so path is an ordinary projected field — filterable and sortable like any
// other — and the path lookup is a filter on it. The projection is the read
// contract's single field vocabulary: an unknown sort or filter name is a
// typed rejection before any SQL renders.
package organization
