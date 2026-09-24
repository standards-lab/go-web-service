// Package organization is the organization domain layer: the recursive
// organization hierarchy, sibling-unique codes composing one unique path
// per node, served under /api/organizations as paginated, filtered, sorted
// reads and four commands, and each organization's logo.
//
// The layer's SQL is the statements/ directory, one authored file per
// statement with its tier header; database.go is the domain's SQL client,
// the sole importer of the query library: it compiles the directory once
// against the service's pattern catalog, registers the inventory, binds
// each statement to a typed handle, and exposes the operations as the
// store's methods. storage.go is the storage translation file, the one
// place the layer calls blobfs or the object store: it sequences the logo's
// protocols over blobfs's steps, the object store's, and the store's image
// statements. No statement, session, or query type crosses out of the two.
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
//
// The logo is ownership at blobfs's file grain: an organization_image row
// binds one file to the organization, and a partial unique index admits one
// active row per organization. The files sit in one structural directory
// under blobfs's root, each named for its id. An upload is a raw body of
// at most 1 MiB in a raster type (SVG is script-capable and refused, 415).
// It writes the pending file and its inactive image in one transaction,
// stores the object outside any, completes the file on the pool, then
// holds the file and makes its image the active one in one transaction,
// and retires the file it replaced by the delete protocol: the image and
// blobfs's delete step in one transaction, the object, then the purge. A
// concurrent replacement that activates first is a unique violation, 409,
// and the losing file is retired. The read serves an available file's
// bytes by proxy, revalidated by its entity tag.
package organization
