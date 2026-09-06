// Package database is the database admin domain: the HTTP half of the
// database admin service, a route group under the admin mount over
// go-database's admin package. The vocabulary, settled by the
// v1.data.sql.prototype experiment:
//
//   - admin mount: the /admin route group, the administrative counterpart
//     of the /api mount;
//   - admin domain: a package under admin/ named for the infrastructure
//     service it administers, the database today;
//   - admin service: the operations, each a trigger over a library
//     function, with their policy (when the seed runs); for the database it
//     is go-database's admin.Service over sqlate's migrator, the session,
//     and the data package's migration set, seeder, catalog, and registry.
//     Startup calls the same functions its endpoints do;
//   - admin handler: the domain's route group, mounted into the admin mount.
//
// The composition root constructs the admin service and mounts this
// package's Routes. The mount's isolation, its own listener, authentication,
// and audit, is the strategy's production constraint and the
// v1.data.sql.integration.listener task; until then the group serves on the
// API listener.
package database
