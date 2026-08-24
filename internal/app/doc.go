// Package app is the composition root: [App] orchestrates the root
// composition from the package's build points — routes.go registers the
// modules the router mounts, and middleware.go declares the router-level
// middleware stack, outermost first. Extending the service means editing a
// build point's body; the signatures, cmd/server, and [App.Run] stay
// untouched.
//
// [New] is the cold start, with no I/O: it constructs infrastructure (each
// service registering on the coordinator where it is constructed), the
// domain over it, and the reactors over both, then assembles the router from
// the registered routes and the middleware stack, and declares the server as
// the coordinator's root-stage service — started after every infrastructure
// stage and drained first, so in-flight requests complete before the
// infrastructure beneath them closes. The probes register on the router's
// native mux, outside every module's middleware, and query the coordinator
// live on every request, aggregating it under the "lifecycle" name ahead of
// its services' own checks. Wiring mistakes panic at construction.
//
// Routes and reactors are the two ways a domain service enters the running
// process: a route is driven by a caller, a reactor by an occurrence the
// process receives or discovers. Both take *domain.Domain; neither is a
// domain service itself.
//
// [App.Run] is the hot start plus shutdown, delegated to the coordinator,
// and returns the process exit code.
package app
