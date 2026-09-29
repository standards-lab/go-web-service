// Package database is the database admin domain: the HTTP half of the
// database admin service, a route group under the admin mount over
// go-database's admin package. The vocabulary, settled by the
// v1.data.sql.prototype experiment:
//
//   - admin mount: the /admin route group, the administrative counterpart
//     of the /api mount;
//   - admin domain: a package under admin/ named for the infrastructure
//     service it administers, the database here and the object store in
//     admin/storage;
//   - admin service: the operations, each a trigger over a library
//     function, with their policy (which set applies at startup); for the
//     database it is go-database's admin.Service over sqlate's migrator,
//     the session, and the data package's migration sets (blobfs's and the
//     service's own), seeder with its named states, catalog, and registry.
//     Startup calls the same functions its endpoints do;
//   - admin handler: the domain's route group, mounted into the admin mount.
//
// The verbs that change the schema (up, down, steps, and the state reset)
// hold the process's quiesce gate exclusively. The package declares the
// gate as SchemaGate and the composition root injects it. The sweep holds
// it shared for each pass, so it never runs a pass under one of those
// verbs. The gate is per process; SchemaGate says what it leaves out.
//
// The composition root constructs the admin service and mounts this
// package's Routes. The mount's isolation, its own listener, authentication,
// and audit, is the strategy's production constraint and the
// v1.admin-listener goal; until then the group serves on the API listener.
package database
