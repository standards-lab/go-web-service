// Package data is the service's infrastructure as the domains see it: the
// database session grouped with the pattern catalog, the object storage
// under blobfs, and the content the admin service administers (the
// migrations, the patterns, and the named states). Neither library may own
// that grouping, and the composition root cannot, because the domains
// would import it; it is a root-level package because the domains import
// it and a root-level package never imports internal/*. Domains and the
// admin service are peers over it; nothing here imports either.
//
// The package's API:
//
//   - [Database], built by [New]: the session, the catalog, and the
//     statements registry; [Database.Lock] takes an advisory lock named in
//     the lock registry ([LockOrganizationTree]).
//   - [Storage], built by [NewStorage]: blobfs's store over [Objects], the
//     adapter over the object store; [Storage.Serve] describes an
//     available file as a [Download], and [Storage.SweepWorker] is the
//     sweep, each pass holding a [SweepGate].
//   - [Migrations] and [AppSet]: the migration sets, blobfs's beneath the
//     service's own.
//   - [Patterns] and [Namespace]: the application's pattern namespace.
//   - [Seeder], built by [NewSeeder] from each domain's [Contribution], a
//     [Seed] of rows or a [FileSeed] of stored files; [SeedRows] decodes a
//     contribution's rows strictly.
//   - [Directives], [Read], [ReadListing], and [Paging]: the lowering of a
//     request's query onto the library's reads and back.
//   - [Status]: the problem matcher over the libraries' errors, whose
//     conflicts carry a Detail constant ([DetailNameTaken] and its
//     siblings) built by [Conflict]; [ErrBodyRead] is an upload whose body
//     failed.
package data
