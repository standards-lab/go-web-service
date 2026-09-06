// Package app is the composition root: [App] assembles the layers into a
// router and a lifecycle coordinator and runs the process. The package is
// one file per layer, so its table of contents is the architecture's layer
// list: infrastructure.go constructs the infrastructure services, admin.go
// the admin services and their mount, domain.go the domain services and
// the API mount, reactors.go the event-driven entry points; routes.go is
// the list of mounts and middleware.go the router-level middleware stack,
// outermost first. Extending the service means editing a layer file's
// body; the signatures, cmd/server, and [App.Run] stay untouched.
//
// [New] is the cold start, with no I/O: it constructs infrastructure (each
// service registering on the coordinator where it is constructed), the
// admin layer over it, the domain over it, and the reactors over both, then
// assembles the router from the mounts and the middleware stack, and
// declares the server as the coordinator's root-stage service, started
// after every numbered stage and drained first, so in-flight requests
// complete before the infrastructure beneath them closes. The stages are
// the ordering rule: the pool at stage 0, the schema at stage 1 (verify,
// apply, verify, seed), the domains at stage 2, each verifying its own
// statements against the migrated schema. The probes register on the
// router's native mux, outside every module's middleware, and query the
// coordinator live on every request. Wiring mistakes panic at construction.
//
// Routes and reactors are the two ways a domain service enters the running
// process: a route is driven by a caller, a reactor by an occurrence the
// process receives or discovers. Both take *Domain; neither is a domain
// service itself.
//
// [App.Run] is the hot start plus shutdown, delegated to the coordinator,
// and returns the process exit code.
package app
