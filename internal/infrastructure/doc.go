// Package infrastructure constructs the services an application is composed
// on into the concrete fields of [Infrastructure] — the logger and the
// database today; storage, auth, or messaging clients as the service grows.
// [New] is the build point: it constructs every service into the struct, in
// dependency order, with provider selection (postgres) made here, and
// registers each one that has a lifecycle on the coordinator it receives, as
// a lifecycle.Service with the stage that places it in the process's startup
// order. Construction opens nothing — connectivity belongs to a service's
// Start hook, so a failed cold start leaks no connections. A binary that
// runs no coordinator (cmd/db) passes a nil coordinator and drives the
// subsystem it uses through the struct's fields directly.
//
// A field either exists or the build fails, so a wiring mistake surfaces at
// compile time; roles sharing a type (a write pool and a read pool) are
// distinct fields, distinguished by name. The struct stops at the
// composition layer: the composition root reads its fields, and domain
// packages receive their dependencies as constructor parameters, never the
// struct itself.
package infrastructure
