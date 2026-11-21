// Package app is the composition root. It describes the service as one
// go-core dependency graph. Each architecture layer has one file, which
// defines the layer's nodes in its define function, so the layer files are
// the architecture's layer list:
//
//   - infrastructure.go: defineInfrastructure, the lifecycle config, the
//     logger, the database pool, the object store, the NATS broker, and
//     the two the domains see the first two through, the sql session and
//     the files store;
//   - telemetry.go: defineTelemetry, the trace and meter providers, which
//     start before the connections and flush after them, the meter
//     provider the service's own instruments are created on, and the
//     sweep metrics, the sweeper's per-pass hook over its instruments,
//     sweep.passes and sweep.backlogged. The meter provider node resolves
//     the otel global at each call rather than holding the SDK provider,
//     which the telemetry node already shuts down. The service.name every
//     signal carries is repeated, character for character, in
//     compose/otel-collector/otel-collector.yaml for its log correlation;
//   - admin.go: defineAdmin, the quiesce gate and the schema service, and
//     the /admin mount;
//   - domain.go: defineDomain, the domain services, and the /api mount;
//   - reactors.go: defineReactors, the general sweeper and the nudge the
//     document layer reaches it through, and the messaging runtime and its
//     relay;
//   - server.go: defineServer, the request edge: the readiness the probes
//     report, the router, and the server;
//   - routes.go: the list of mounts;
//   - middleware.go: the router-level middleware stack, outermost first.
//
// [Nodes] holds one handle per node, the single description of what the
// service is composed of: each define function fills its own part of one
// Nodes value, and its constructors read the lower layers' nodes from it
// with Use. Each field's node is named for the field. [App] is the
// described process. [New] is the cold start: it defines the configuration
// and the logger's writer as fixed-value nodes, describes the rest of the
// graph layer by layer, constructs nothing, and cannot fail. [App.Graph]
// and [App.Nodes] publish the graph and its handles, so a caller can
// Observe or Replace a node before Run, as internal/apptest's fixtures do.
// [App.Run] is the hot start plus shutdown: it runs a go-web-sdk
// web.Service, which builds [App.Roots], the server and the reactors group,
// plus the lifecycle config node, hands the System to go-core's lifecycle
// Coordinator, and returns the process exit code. Run reports a
// constructor's error on stderr as the service failing; a wiring mistake
// panics during Build.
//
// What a node takes part in is inferred from its value's methods: a
// lifecycle.Starter or Stopper is started or stopped, a Subsystem is both,
// a Starter that is also a ReadinessChecker (a lifecycle.ReadyStarter)
// joins the readiness probe, though a Ready alone does not, and a Monitored
// has its runtime error watched. Nodes start in layer order and drain in
// reverse.
// No layer file registers anything with the Coordinator, and no
// constructor opens a connection: connectivity belongs to each
// participant's Start, so a failed Build leaks none. The order is
// computed from what each node uses, plus seven After edges, each declared
// with graph.After on the later node's Define line and each ordering two
// nodes without passing a value. The database, the object store, and the
// broker start after telemetry. The sweeper and the relay start after the
// schema. The server starts after the reactors group, which orders it
// after every reactor, and after the schema, so it is alone in the top
// layer, starts last, and drains first. The drain therefore runs the
// server, then the reactors (the relay with its last pass, which publishes
// what the server's requests committed while they drained), then the
// schema, then the broker and the connections. The probes report the
// Coordinator's status under the "lifecycle" name, then the database, the
// object store, the broker, the schema, the sweeper, and the relay, in
// layer, then definition, order. [Telemetry] and [Nudge] are the two node
// values the package owns: telemetry as a participant whose shutdown never
// fails the run, and the sweeper's nudge, which has no Ready, so the probe
// gains no check for it.
//
// Routes and reactors are the two ways work enters the running process: a
// caller drives a route, and an occurrence drives a reactor. A reactor
// commonly dispatches to a domain service; the sweeper's dispatches to its
// reclaimers, the libraries' own (docs/sweep.md), and the
// relay's publishes to the broker. The relay's last pass publishes what
// was committed while the producers drained, so every producer drains
// before it: a request-driven producer, as every domain service is, needs
// no edge, since the server drains first, and a node that commits events
// outside a request, such as a consumer's reactor, Uses the relay's node.
//
// The middleware stack runs tracing outermost, so every later middleware
// and the handler see the request inside its span; its Use of the
// telemetry node is what brings telemetry into the Build. RequestID comes
// next, sourcing the span's trace id, so RequestLogger's record, the
// X-Request-Id header, and a problem's request_id carry one value.
// Recoverer follows RequestLogger, so a panic is answered and logged as a
// 500 carrying that id. RateLimit sits innermost, so a rejected request is
// logged with its id and 429, and skips the probes. Middleware reads
// infrastructure nodes only; one that needs a domain service is domain
// logic, and belongs in a route or a reactor.
//
// Extending the service means defining a node in its layer's
// define function, and placing a reactor's node in the reactors group with
// graph.In at its Define; the signatures, cmd/server, and [App.Run] stay
// untouched.
package app
