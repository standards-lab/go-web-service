// Package document is the document domain layer: each organization's
// hierarchy of directories and files over blobfs and the object store,
// served under /api/documents/{org}.
//
// An organization owns one document root, a top-level blobfs directory an
// owner row (organization_directory, migrations 0003 and 0004) binds to
// it; everything beneath the root is the organization's by containment.
// Every route checks an id's scope against that root first, so an id
// outside it is not found, as an absent one is. The root's owner row
// cascades with its directory, so the sweep that removes a marked branch
// needs no hook from this layer. The statuses on the wire are the layer's
// own vocabulary, mapped from blobfs's, so a library rename never changes
// the wire.
//
// The package's API:
//
//   - [Service], built by [New]: the layer's operations, one per endpoint.
//   - [Sweeper]: the nudge the layer gives the sweep after marking a branch.
//   - [Recorder]: the recorder the layer emits its events through.
//   - [DirectoryCreated] and [DirectoryMoved], with [DirectoryData], and
//     [DirectoryDeleted], with [DirectoryDeletedData]: the events the
//     directory commands emit, each in the transaction that makes its
//     change true; the document root's DirectoryCreated commits in the
//     transaction that creates the root on the organization's first write.
//   - [FileUploaded] and [FileMoved], with [FileUploadedData], and
//     [FileDeleted], with [FileDeletedData]: the events the file commands
//     emit: an upload's in the transaction that makes its file available,
//     a move's in the move's, and a delete's in the transaction that
//     begins the delete.
//   - [Routes]: the route group the composition root mounts.
//   - [Directory], [File], [Content], [Identity]: the read shapes, the
//     download, and a command's result.
//   - [DirectoryStatus] ([DirectoryActive], [DirectoryDeleting]) and
//     [FileStatus] ([FilePending], [FileAvailable], [FileDeleting]): the
//     statuses a read carries.
//   - [CreateDirectory], [MoveDirectory], [MoveFile]: the command inputs,
//     each validating itself.
//   - [RootAlias]: the id segment that names the document root.
//   - [ErrValidation]: a command input's rejection, a 400.
package document
