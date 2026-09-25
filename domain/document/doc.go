// Package document is the document domain layer: each organization's
// hierarchy of directories and files, served under
// /api/documents/{org} as reads of one directory's contents, metadata
// reads, uploads, proxied downloads, moves, and deletes.
//
// The layer's SQL is the statements/ directory, the owner row's four
// statements; database.go is the domain's SQL client, the sole importer of
// the query library: it compiles the directory against the service's
// pattern catalog, registers the inventory, binds each statement to a
// typed handle, and lowers a request's query onto blobfs's listings.
// storage.go is the storage translation file, the one place the layer calls
// blobfs or the object store: it runs the scope check and sequences the
// write, delete, and move protocols over blobfs's steps, the object
// store's, and the owner row's statements. No statement, session, or query
// type crosses out of the two. entities.go owns the shapes and their
// rules; service.go is the direct map from endpoint to operation; handler.go
// binds the service to the route group the composition root mounts into
// the API module.
//
// Ownership is at blobfs's directory grain: an organization_directory row
// binds one top-level directory, named with the organization's id, to the
// organization as its document root, and everything beneath it is the
// organization's by containment. A directory id segment may be the literal
// root, the alias of that directory. A write that needs the root ensures it
// on first use, the directory and its owner row in one transaction after
// the organization is read; a read before then answers an empty listing, or
// not found for a specific id. Every route that takes an id checks its
// scope first: it reads the owner row, then asks blobfs whether the
// directory, or the file's directory, lies within the root. An id outside
// is not found, indistinguishable from an absent one, so no request reads,
// moves, or deletes across organizations, and a move stays under one root.
// The root itself is not moved.
//
// An upload is a raw body of at most 10 MiB in any media type, stored by
// blobfs's write protocol: the pending row, the put, the completion; a
// taken name is a conflict. A download is proxied as an attachment, never
// rendered inline, since stored HTML would run in the API's origin. A file
// delete is blobfs's delete protocol; a directory delete removes an empty
// directory, or with recursive=true walks it first, a bounded walk that
// removes every file by the delete protocol and every directory after its
// contents, deepest first. Removing the root removes its owner row in the
// same transaction. The guarded moves take their version from If-Match.
package document
