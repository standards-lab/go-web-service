// Package document is the document domain layer: each organization's
// hierarchy of directories and files, served under
// /api/documents/{org} as reads of one directory's contents, metadata
// reads, uploads, proxied downloads, moves, and deletes.
//
// The layer's SQL is the statements/ directory, the owner row's statements
// and the seed's path walk; database.go is the domain's SQL client, the
// sole importer of the query library: it compiles the directory against the
// service's pattern catalog, registers the inventory, and binds each
// statement to a typed handle. storage.go is the storage translation file,
// the one place the layer calls blobfs or the object store. It runs the
// scope check; runs the write and the delete as blobfs's two-phase
// protocols over the data package's object store, with the scope check as
// their first transaction's step, and the download through the data
// package's Serve; reads blobfs's listings through the data package's
// lowering of a request's query; and sequences the moves and the
// directories over blobfs's steps and the owner row's statements. No
// statement, session, or query type crosses out of the two. entities.go
// owns the shapes and their rules; service.go is the direct map from
// endpoint to operation; handler.go binds the service to the route group
// the composition root mounts into the API module.
//
// Ownership is at blobfs's directory grain: an organization_directory row
// binds one top-level directory, named with the organization's id, to the
// organization as its document root, and everything beneath it is the
// organization's by containment. The document layer owns that table,
// though it names the organization, because the row is the owner row at
// the directory grain: it binds a directory, and only this layer reads or
// writes it, as the organization layer owns organization_image, its owner
// row at the file grain. A directory id segment may be the literal root,
// the alias of that directory. A write that needs the root ensures it
// on first use, the directory and its owner row in one transaction after
// the organization is read; a read before then answers an empty listing
// once the organization is read, or not found for a specific id, and a
// listing for an organization that does not exist is not found, as every
// route answers it. Every route that takes an id checks its
// scope first: it reads the owner row, then asks blobfs whether the
// directory, or the file's directory, lies within the root. An id outside
// is not found, indistinguishable from an absent one, so no request reads,
// moves, or deletes across organizations, and a move stays under one root.
// The root itself is not moved.
//
// The owner row goes with its directory: its foreign key into
// blobfs_directory cascades, so whatever removes a root, the plain delete
// of an empty root or the sweep of a recursive delete, removes the row in
// the same statement, and the layer runs no statement and gives the sweep
// no hook for it. blobfs forbids a cascade on its own foreign keys, since
// one would remove file rows whose objects still exist and leave those
// objects unreachable; that rule does not reach a consumer's key, and an
// owner row holds no object, so its cascade strands nothing. A
// consumer-side cascade also scales where the sweep's hook would not:
// blobfs keeps one removal hook, so a second directory-grain layer
// registering its own would silently replace the first.
//
// A conflict is a 409 with a curated detail, never the error's own text,
// which would name blobfs's operation, ids, and constraints: "an entry
// with that name already exists", "the directory is not empty", "the
// directory is being deleted", "the file is being deleted", "the file is
// referenced", or otherwise "the request conflicts with the current state"
// (data.Status's Detail constants). blobfs refuses a change as deleting
// with a blobfs.DeletingError that says whose delete refused it: a move of
// a file whose own delete began, in a directory that is not deleting, is
// "the file is being deleted", and one in a marked branch or into a
// deleting directory is the directory's. An upload is refused the same
// way when its row's delete began before it completed. A file delete is
// never refused as deleting: a repeated delete converges.
//
// A directory reads with its status, active until blobfs marks its branch
// deleting, and a file with its stage, pending, available, or deleting.
// The statuses are the layer's own vocabulary (DirectoryStatus and
// FileStatus), mapped from blobfs's in storage.go, so the wire never
// carries a library type and a change to the library's names never changes
// the wire. A status the layer does not name, one blobfs added, is a
// server fault (500) on the read that meets it, never passed through. A deleting branch's directories and files still
// read by id, with their status, while every listing hides the branch: its
// parent lists without it, and a listing of a directory within it is not
// found.
//
// An upload is a POST of a raw body of at most 10 MiB in any media type
// to its directory's files, the new file's name in the required name
// query parameter, answered 201 with the file's metadata read as its
// Location. It is a POST, not a PUT to the name, because it creates a new
// file under an id the server mints and is not idempotent: a retry after a
// lost response is a second create, refused with the name's conflict.
// blobfs's two-phase write stores it in three steps: the pending row, the
// put, and the completion. A put or a completion that fails abandons the
// pending row, and a taken name is a conflict. A completion refused because a mark
// or the sweep reached the row deletes the object just put, since a sweep
// that ran before the put landed could not have deleted it. A download is
// proxied as an attachment, never rendered inline, since stored HTML would
// run in the API's origin.
//
// Two validators, the version and the object ETag, share HTTP's entity-tag
// syntax, and neither stands in for the other. A row's version, the
// integer each of its changes advances, is the version field of every read
// and command body; no row is sent with an ETag header, and a client
// quotes the version it read as the If-Match of a move or a delete,
// directory or file ("3"). Those act on a row the client read and another
// request may have changed since, so a missing header is 428, a weak or
// non-numeric tag 400, and a stale version 412. A create or an upload
// names a new entry, with no version to hold; a taken name is its
// conflict. The download is a different resource from
// the file's row: its object ETag is the object store's tag for the bytes
// and its Last-Modified the row's last change, and a conditional GET
// naming either is answered 304 without opening the object. The object
// ETag is never a version, and If-Match refuses one as malformed.
//
// The layer seeds document hierarchies. Seed is its seed contribution, a
// file seed, to the data package's named states, which the composition
// root hands the seeder after the organizations'. A state names each tree by
// its organization's path, resolved a code at a time by the layer's own
// statement, with a fixed id for the root and for every entry, and the
// files' content inline. The seeder runs it once the organizations commit,
// since a file's object is put outside any transaction. The root is
// ensured as a first write ensures it, the directories by blobfs's
// insert-or-find on the pool, and the files by the two-phase write's
// retry-safe form, blobfs's Store.Ensure, so a rerun finds every entry and
// a reset writes each file again under the same key. An entry already
// there by name keeps its own id and content, and a client's file there
// under another id, pending or complete, is never written over; one a
// client moved or renamed, whose id a row holds under another name, and
// one being deleted are left as they stand.
//
// A file delete is blobfs's two-phase delete, 204. A directory delete
// removes an empty directory, 204; removing the root removes its owner row
// with it. With recursive=true it deletes the branch, the directory with
// everything beneath it, in two stages: blobfs marks the branch deleting in
// one transaction, the request answers 202 with the directory's read as its
// Location, and the sweep the service is given is nudged after the commit
// to remove the rows and their objects. Until the sweep finishes, the
// directory reads deleting, its listings are not found, and a write into it
// is a conflict. A repeated recursive delete is the mark's retry, accepted
// again at any version. The root's branch is marked like any other; its
// owner row stands until the sweep removes the root, and goes with it.
// Either way, the organization then has no root until its next write
// ensures a new one.
package document
