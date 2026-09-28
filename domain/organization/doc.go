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
// place the layer calls blobfs or the object store: it runs the logo's
// write, delete, and read through the data package's shared file protocols
// and sequences the activation over blobfs's steps and the store's image
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
// missing header is 428, a stale version 412; state conflicts are 409,
// whose detail is a fixed text and never the error's own: a taken code, a
// missing parent, a cycle, and a concurrent logo replacement all read "the
// request conflicts with the current state".
// The path is projected at read time by the lineage CTE (standard
// SQL:1999) and is an ordinary contract field, filterable and sortable
// like any other.
//
// The layer seeds its own table: Seed is its contribution to the data
// package's named states, which the composition root hands the seeder. It
// applies a state's organizations, each naming its parent by code, parents
// first, by the layer's seed statements: an insert that leaves a sibling's
// taken code as it stands, and the lookup that finds that row, so a seed
// is idempotent. LogoSeed is its second contribution, of stored files: the
// logos a state names, each an organization's path, a fixed file id, and
// a fixture under the data package's seeds/fixtures/. The seeder runs it
// once the organizations commit, since a logo's object is put outside any
// transaction. A fixture passes the upload's rules before any I/O; an
// organization with an active logo is left alone; otherwise the file is
// written under its fixed id by the shared write protocol's retry-safe
// form and activated, in storage.go, beside the logo's other protocols.
// The seed leaves alone what it does not own: a file under the id that
// another organization's image binds, a file being deleted, and a logo
// that became active meanwhile, the file it stored for it retired.
//
// The logo is ownership at blobfs's file grain: an organization_image row
// binds one file to the organization, and a partial unique index admits one
// active row per organization. The files sit in one structural directory
// under blobfs's root, each named for its id. An upload is a raw body of
// at most 1 MiB in a raster type (SVG is script-capable and refused, 415).
// It is written by the data package's shared write protocol: the pending
// file, created once the organization is read, alone in one transaction,
// the object stored outside any, the file completed on the pool. The image
// is written only after the file completes, so a write that stops partway
// leaves a plain pending row that the protocol abandons or blobfs's stale
// reclaim removes, and never an image that would refuse the reclaim's
// purge and the organization's delete. The completed file is then held and
// activated in one transaction that removes the replaced logo's image,
// begins its file's delete, and inserts the new image as the active one;
// the replaced file's object and row are purged after the commit. A
// concurrent replacement that activates first is a unique violation, 409,
// and the losing file is retired by the shared delete protocol, as a logo
// delete retires the active one. The read serves an available file's bytes
// by proxy through the shared read, revalidated by its entity tag.
//
// The layer's validators are two, sharing HTTP's entity-tag syntax. An
// organization's version, the integer each command advances, is the
// version field of its reads and command bodies, never an ETag header, and
// a client quotes the version it read as the If-Match of an edit, a
// transfer, or a delete ("3"). The logo takes no version: it is a
// singleton addressed by its organization, so its PUT replaces whatever is
// active and its DELETE retires whatever is, the last write winning, and
// two replacements racing to activate are told apart by the conflict
// above, not a precondition. Its read's ETag is the object store's tag for
// the active logo's bytes, with Last-Modified the file's last change, and a
// conditional GET naming either answers 304; a replacement is a new object
// with a new tag. The PUT answers 201 Created with the logo's Location and
// the new file's id, and no version, whether or not it replaced a logo:
// every PUT creates a new file.
package organization
