// Package organization is the organization domain layer: the recursive
// organization hierarchy, each node's code unique among its siblings so
// the composed path is unique, served under /api/organizations as paged,
// filtered, sorted reads, four commands, and each organization's logo.
//
// The path is projected at read time by a lineage CTE and is an ordinary
// field of the read model. A command returns identity and version only;
// the guarded ones (edit, transfer, delete) take their version from
// If-Match. The logo is ownership at blobfs's file grain: an
// organization_image row binds one file to the organization, with at most
// one active per organization, and a PUT replaces whatever is active, the
// last write winning.
//
// Each command emits one event through the messaging runtime's recorder,
// in the transaction that makes its change true, so a command that fails
// commits none. A seed that creates an organization or activates a logo
// emits OrganizationCreated or LogoUploaded, with the data the command's
// carries, in the transaction that creates it, and nothing for one it
// finds already there; the reads and the purge that finishes a logo's
// delete emit nothing.
//
// The package's API:
//
//   - [Service], built by [New]: the layer's operations, one per endpoint.
//   - [Recorder]: the recorder the layer emits its events through.
//   - [OrganizationCreated], [OrganizationEdited], [OrganizationTransferred],
//     [OrganizationDeleted], [LogoUploaded], [LogoDeleted], with
//     [OrganizationData], [OrganizationDeletedData], [LogoUploadedData], and
//     [LogoDeletedData]: the events the commands emit, each in the
//     transaction that makes its change true.
//   - [Routes]: the route group the composition root mounts.
//   - [Organization] and [Logo]: the read shape and the logo's download.
//   - [CreateOrganization], [EditOrganization], [TransferOrganization]: the
//     command inputs, each validating itself.
//   - [Identity] and [LogoIdentity]: a command's result and a logo PUT's.
//   - [ErrValidation]: a command input's rejection, a 400.
//   - [ErrCycle]: a transfer into the organization's own subtree, a 409.
package organization
