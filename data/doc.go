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
// namespace (patterns/), and the named states with their seed statements
// (seeds/, statements/), each behind a function the admin service
// triggers: Migrations, Patterns, and a Seeder. Migrations declares the sets
// the libraries beneath the service ship, as the composition root passes
// them, ahead of the service's own. Beside them sit the pieces every domain
// would otherwise copy: the advisory-lock name registry, the lowering from
// the web SDK's query to the library's directives and the collection read
// it addresses, and the status matcher over the libraries' error
// vocabulary, the storage libraries' included, which gives every conflict
// a fixed detail (the Detail constants) and never the error's own text.
//
// Storage is the object storage infrastructure as the domains see it:
// blobfs's store over the same session, and Objects, the adapter that is
// the one place the service names its object-store library. Storage also
// runs the file protocols every domain shares, staged for promotion to
// blobfs: Write, blobfs's two-phase write with its abandon and its writer
// rule; Retire and Purge, its two-phase delete; and Serve, the read of an
// available file. A domain enters them through a callback run in the
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
// blobfs beside Sweep.
//
// Domains and the admin service are peers over this package; nothing here
// imports either.
package data
