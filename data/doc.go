// Package data is the service's database infrastructure as the domains
// see it: the session every statement runs through grouped with the
// pattern catalog the statements compile against, the dialect reachable
// through the session, and the content the admin service administers.
// Neither library may own the grouping: sqlate must not know the pool,
// go-database must not know patterns, and the composition root cannot,
// because the domains would import it. It is a root-level package, beside
// domain/ and admin/, because the domain packages import it and a
// root-level package never imports internal/*.
//
// The content is the schema (migrations/), the application's pattern
// namespace (patterns/), and the named states (seeds/, with the files a
// state names under seeds/fixtures/), each behind a function the admin
// service triggers: Migrations, Patterns, and a Seeder. The Seeder
// composes the domains' seed contributions, each declared by the domain
// over its own tables and statements and handed in by the composition
// root, so a state file is read here and every contribution's rows are
// applied by the domain that owns them. A contribution is of one of two
// kinds. A Seed's rows apply in the seed's one transaction. A FileSeed's
// rows are stored files, whose objects blobfs's two-phase write puts
// outside any transaction, after the pending row commits, so the Seeder
// runs every FileSeed once that transaction commits, the rows the files
// name standing by then, and each file goes through Storage.Ensure, the
// shared write protocol's retry-safe form. A seeded file carries a fixed
// id, so a rerun finds it and a reset writes it again under the same key,
// its put replacing whatever object the reset left in the container.
// Migrations declares the sets
// the libraries beneath the service ship, as the composition root passes
// them, ahead of the service's own. Beside them sit the pieces every domain
// would otherwise copy: the advisory-lock name registry, the lowering from
// the web SDK's query to the library's directives and the collection read
// it addresses, and the status matcher over the libraries' error
// vocabulary, the storage libraries' included, which gives every conflict
// a fixed detail (the Detail constants) and never the error's own text.
//
// Storage is the object storage infrastructure as the domains see it:
// blobfs's store over the same session, and Objects, the adapter over the
// started object store, so no domain names the object-store library. The
// library is named where it is composed and administered: by the
// composition root, the storage admin domain, and the status matcher,
// which reads its errors. Storage also
// runs the file protocols every domain shares, staged for promotion to
// blobfs: Write, blobfs's two-phase write with its abandon and its writer
// rule, and Ensure, its retry-safe form over blobfs's insert-or-find,
// which two writers of one fixed id may share; Retire and Purge, its
// two-phase delete; and Serve, the read of an available file. A domain enters them through a callback run in the
// protocol's first transaction, so no domain concept reaches here.
//
// Storage.SweepWorker is the storage infrastructure's background worker:
// blobfs's sweep, run in passes while a pass reports more, which finishes
// the branch deletes the domains mark and reclaims stale rows, with the
// service's policy for a pass's refusals (logged at warn, never returned).
// The architecture defines a Reactor as an entry point that calls a Domain
// Service; this worker calls none, so it is not one. It is the exception
// the composition root stages as a reactor anyway, since the reactor is
// the process's one runner for work that lasts the process lifetime. Its
// pass loop is kept apart from the logging policy, staged for promotion to
// blobfs beside Sweep. Each pass holds the process's quiesce gate shared,
// declared here as SweepGate, so no pass runs under an admin verb that
// changes the schema, which holds it exclusively.
//
// Domains and the admin service are peers over this package; nothing here
// imports either.
package data
