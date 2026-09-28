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
// A conflict is a 409 whose detail is a fixed text, never the error's own,
// which would name blobfs's operation, ids, and constraints: "an entry
// with that name already exists", "the directory is not empty", "the
// directory is being deleted", "the file is referenced", or otherwise "the
// request conflicts with the current state" (data.Status's Detail
// constants).
//
// A directory reads with its status, active until blobfs marks its branch
// deleting. A deleting branch still reads by id, directories and files
// with their status, while every listing hides it: its parent lists
// without it, and a listing of a directory within it is not found.
//
// An upload is a raw body of at most 10 MiB in any media type, stored by
// blobfs's write protocol: the pending row, the put, the completion; a
// taken name is a conflict. A download is proxied as an attachment, never
// rendered inline, since stored HTML would run in the API's origin. A
// completion refused because a mark or the sweep reached the row deletes
// the object just put, since a sweep that ran before the put landed could
// not have deleted it.
//
// Every move and delete takes its version from If-Match: a missing header
// is 428, a stale version 412. A file delete is blobfs's delete protocol,
// 204. A directory delete removes an empty directory, 204; removing the
// root removes its owner row in the same transaction. With recursive=true
// it deletes the branch, the directory with everything beneath it, in two
// stages: blobfs marks the branch deleting in one transaction, the request
// answers 202 with the directory's read as its Location, and the sweep the
// service is given is nudged after the commit to remove the rows and
// their objects. Until the sweep finishes, the directory reads deleting,
// its listings are not found, and a write into it is a conflict. A
// repeated recursive delete is the mark's retry, accepted again at any
// version. The root's branch is marked like any other; its owner row
// stands until the sweep removes the root.
package document
