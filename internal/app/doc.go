// Package app is the composition root: [App] assembles the layers into a
// router and a lifecycle coordinator and runs the process. The package is
// one file per layer, so its table of contents is the architecture's layer
// list: infrastructure.go constructs the infrastructure services,
// telemetry.go the telemetry service, admin.go the admin services and their
// mount, domain.go the domain services and the API mount, reactors.go the
// event-driven entry points; stages.go is the stage table every layer file
// registers from, routes.go the list of mounts, and middleware.go the
// router-level middleware stack, outermost first.
// Extending the service means editing a layer file's body; the signatures,
// cmd/server, and [App.Run] stay untouched.
//
// [New] is the cold start, with no I/O: it constructs infrastructure (each
// service registering on the coordinator where it is constructed), the
// domain over it, the admin layer over both, since its seeder composes the
// domains' seed contributions, and the reactors, then
// assembles the router from the mounts and the middleware stack, and
// declares the server as the coordinator's root-stage service, started
// after every numbered stage and drained first, so in-flight requests
// complete before the infrastructure beneath them closes. The stages are
// the ordering rule, and the stage table in stages.go is their one
// declaration: the pool and the object store at stageInfrastructure, the
// schema at stageSchema (verify, apply, verify, seed, the seed writing its
// files to the object store started a stage earlier; go-database's
// admin.Stage, named in the table), blobfs's store and the domains at
// stageVerify, each verifying its own statements against the migrated
// schema, the sweep reactor at stageReactors, started once the tables it
// sweeps are verified and drained after the server and before everything
// beneath it, and the server at stageRoot. A stage is the root's decision:
// no domain declares one or imports the lifecycle, and each layer file
// registers what it constructs at a stage the table names. Telemetry holds
// no stage: it starts in a startup hook before the first stage and stops
// in a shutdown hook after the last, so it brackets every numbered stage. The probes
// register on the router's native mux, outside every module's middleware,
// and query the coordinator live on every request. Wiring mistakes panic
// at construction.
//
// Routes and reactors are the two ways a domain service enters the running
// process: a route is driven by a caller, a reactor by an occurrence the
// process receives or discovers. Routes take *Domain, and a reactor takes
// it for the domain call each occurrence dispatches to. Neither is a
// domain service itself. A reactor is the staged sdk reactor, registered
// with lifecycle's Add and its Err passed to Monitor.
//
// The sweep is the exception: it is staged as a reactor but calls no
// domain service. It runs data's background worker (Storage.SweepWorker),
// which finishes blobfs's deletes and reclaims its stale rows over the
// storage infrastructure alone, since a document root's owner row goes
// with its directory through the row's cascading foreign key and needs no
// domain call. The reactor is the one runner the process has for work that
// lasts the process lifetime, so the worker runs on it, and reactors.go
// holds only its declaration: the options from cfg, the source, the stage,
// and the registration. Its source is built before the domain and handed
// to both halves: the sweep's wake, which the document layer nudges after
// each branch it marks and the sweep reactor receives from. The wake is
// nudged once at construction, so the sweep runs at startup and a branch
// marked before a restart does not wait an interval. The quiesce gate is
// built the same way, before both halves that meet in it: the admin
// layer's schema-changing verbs hold it exclusively and each sweep pass
// holds it shared, so a reset never deadlocks with a pass. It orders this
// process's work only; a reset is a development operation, and one across
// replicas would need a database lock.
//
// [App.Run] is the hot start plus shutdown, delegated to the coordinator,
// and returns the process exit code.
package app
