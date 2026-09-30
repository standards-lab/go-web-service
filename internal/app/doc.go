// Package app is the composition root: [App] assembles the layers into a
// router and a lifecycle coordinator and runs the process. The package is
// one file per layer, so its table of contents is the architecture's layer
// list: infrastructure.go, telemetry.go, admin.go, domain.go, and
// reactors.go construct their layers; stages.go is the stage table every
// layer file registers from, routes.go the list of mounts, and
// middleware.go the router-level stack, outermost first. Extending the
// service means editing a layer file's body; the signatures, cmd/server,
// and [App.Run] stay untouched.
//
// A stage is the root's decision: no domain declares one or imports the
// lifecycle. Telemetry holds no stage; its startup and shutdown hooks
// bracket every stage. Routes and reactors are the two ways a domain
// service enters the running process, a route driven by a caller and a
// reactor by an occurrence; the sweep runs on a reactor though it calls no
// domain service (the README's Sweep section). Wiring mistakes panic at
// construction.
//
// The package's API:
//
//   - [App], built by [New], the cold start with no I/O; [App.Run] is the
//     hot start and the drain, returning the exit code.
//   - [Infrastructure], [Domain], and [Admin]: the layers the layer files
//     build and the router's mounts read.
package app
