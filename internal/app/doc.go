// Package app is the composition root: [App] assembles the layers into a
// router and a lifecycle coordinator and runs the process. The package is
// one file per layer, so its table of contents is the architecture's layer
// list: infrastructure.go constructs the infrastructure services,
// telemetry.go the telemetry service, admin.go the admin services and their
// mount, domain.go the domain services and the API mount, reactors.go the
// event-driven entry points; routes.go is the list of mounts and
// middleware.go the router-level middleware stack, outermost first.
// Extending the service means editing a layer file's body; the signatures,
// cmd/server, and [App.Run] stay untouched.
//
// [New] is the cold start, with no I/O: it constructs infrastructure (each
// service registering on the coordinator where it is constructed), the
// admin layer over it, the domain over it, and the reactors, then
// assembles the router from the mounts and the middleware stack, and
// declares the server as the coordinator's root-stage service, started
// after every numbered stage and drained first, so in-flight requests
// complete before the infrastructure beneath them closes. The stages are
// the ordering rule: the pool at stage 0, the schema at stage 1 (verify,
// apply, verify, seed), the domains at stage 2, each verifying its own
// statements against the migrated schema, and the sweep reactor at stage
// 3, started once the tables it sweeps are verified and drained after the
// server and before the domains and the pool. Telemetry holds no stage number:
// it starts in a startup hook before stage 0 and stops in a shutdown hook
// after the last stage, so it brackets every numbered stage. The probes
// register on the router's native mux, outside every module's middleware,
// and query the coordinator live on every request. Wiring mistakes panic
// at construction.
//
// Routes and reactors are the two ways a domain service enters the running
// process: a route is driven by a caller, a reactor by an occurrence the
// process receives or discovers. Routes take *Domain; a reactor takes it
// once it dispatches to a domain call, which the sweep does not, since a
// document root's owner row goes with its directory through the row's
// cascading foreign key. Neither is a domain service itself. A reactor is the staged sdk reactor, registered with
// lifecycle's Add and its Err passed to Monitor. A source a domain signals
// is built before the domain and handed to both halves: the sweep's wake,
// which the document layer nudges and the sweep reactor receives from.
//
// [App.Run] is the hot start plus shutdown, delegated to the coordinator,
// and returns the process exit code.
package app
