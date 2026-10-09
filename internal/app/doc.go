// Package app is the composition root. It describes the service as one
// go-core dependency graph, and each architecture layer has one file, which
// defines the layer's nodes in its define function and owns its mount, so
// the layer files are the architecture's layer list:
//
//   - infrastructure.go: defineInfrastructure, the configuration, the
//     logger, the database pool, the object store, and the two the domains
//     see them through, the sql session and the files store;
//   - telemetry.go: defineTelemetry, the trace and meter providers, which
//     bracket the connections;
//   - admin.go: defineAdmin, the quiesce gate and the schema service, and
//     the /admin mount;
//   - domain.go: defineDomain, the domain services, and the /api mount;
//   - reactors.go: defineReactors, the sweep's wake and its sweeper;
//   - server.go: defineServer, the request edge: the readiness the probes
//     report, the router, and the server;
//   - routes.go: the list of mounts;
//   - middleware.go: the router-level middleware stack, outermost first.
//
// [Nodes] holds one handle per node, the single description of what the
// service is composed of: each define function fills its own part of one
// Nodes value, and its constructors read the lower layers' nodes from it
// with Use. [App] is the described process. [New] is the cold start: it
// describes the graph, layer by layer, lowest first, constructs nothing,
// and cannot fail. [App.Graph] and [App.Nodes] publish the graph and its
// handles, so a caller can Observe or Replace a node before Run. [App.Run]
// is the hot start plus shutdown: it builds the graph from the config, the
// logger, the server, the schema, and each reactor as roots, hands the
// System to go-core's lifecycle Coordinator, and returns the process exit
// code. A constructor's error is reported there as the service failing; a
// wiring mistake panics during Build.
//
// What a node takes part in is inferred from its value's methods: a
// lifecycle.Starter or Stopper is started or stopped, a Subsystem is both,
// a ReadinessChecker joins the readiness probe, and a Monitored has its
// runtime error watched. Nodes start in layer order and drain in reverse.
// No layer file registers anything with the Coordinator. The order is
// computed from what each node uses, plus three orderings without a value:
// the database and the object store after telemetry, the sweeper after the
// schema, and the server after the sweeper and the schema, so it is alone
// in the top layer, starts last, and drains first. The probes report the
// Coordinator's status under the "lifecycle" name, then the database, the
// object store, the schema, and the sweeper, in layer, then definition,
// order. [Telemetry] and [Wake] are the two node values the package owns:
// telemetry as a participant whose shutdown never fails the run, and the
// sweep's wake, which has no Ready, so the probe gains no check for it.
//
// Routes and reactors are the two ways work enters the running process: a
// caller drives a route, and an occurrence drives a reactor. A reactor
// commonly dispatches to a domain service; the sweep's dispatches to none
// (the README's Sweep section). Extending the service means defining a node
// in its layer's define function, and appending a reactor's node to
// Nodes.Reactors; the signatures, cmd/server, and [App.Run] stay untouched.
package app
