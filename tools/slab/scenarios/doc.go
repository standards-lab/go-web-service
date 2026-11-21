// Package scenarios holds slab's narrated scenarios on go-cli-sdk's
// scenario runner, each run as "slab scenario <name>". The package is
// built over the node handles the composition root hands it, in [Nodes]:
// each scenario declares the nodes it reads, need nodes first and then the
// settings, and its steps read their values from the invocation, so a
// scenario brings up only what it needs. The sqlate scenario runs a
// library's mechanism in process over the service's own sources on disk,
// with nothing running. The domain scenario runs the organization domain's
// full CRUD surface against the running service, reseeded from a known
// fixture on each run. The problems scenario sends the running service one
// request per error condition it answers with a problem document. The
// storage scenario walks the service's stored files: the migration sets, a
// logo's replace cycle, a document tree and a cursor walk, the scoped
// refusals, a recursive delete waited out to 404, and a reset. It starts
// with an additive seed rather than a reset, and names what it creates for
// the run, so it runs twice in a row without a reset between. The events
// scenario sends one command for each event the service publishes, each
// step naming the events "slab events watch" prints for it in another
// terminal; it too names what it creates for the run, and ends by deleting
// its organization's document root and then the organization. The sweeper
// scenario reads the general sweeper's three reclaimers through their
// metrics in Mimir, by way of Grafana's datasource proxy (httpx.Mimir),
// around a recursive delete that nudges the files reclaimer; it
// names its branch for the run and its delete removes it.
//
// The five scenarios against the running service declare the service
// node, whose value is a [Service]; the two that point at traces and the
// one that reads metrics declare the grafana up node too, whose value is a
// [Grafana], carrying a Mimir client bound to Grafana. Each value is a
// lifecycle Starter that probes what it names, so a run whose stack is down
// fails before its first step, its report naming the mise task that brings
// the stack up. A [Service] also carries the client the composition root
// binds to the service, which the scenarios call it through; they
// construct no client of their own.
//
// The package exports:
//
//   - [Nodes], the graph nodes the scenarios are built over
//   - [Service] and [Grafana], the values of the need nodes
//   - [All], the scenarios in presentation order
//   - [Sqlate], [Domain], [Problems], [Storage], [Events], and [Sweeper],
//     the scenarios
//
// scenarios.go holds Nodes, All, and the narration helpers every
// scenario's steps share, and stack.go holds the need nodes' values.
// calls.go holds a call or check only when two scenarios make it
// identically; a step whose point is showing its own literal request keeps
// that request in its scenario file. Each other file holds one scenario:
// its constructor, its literal, and the step methods that run it.
//
// A step narrates its prose with the reporter's Note and shows its results
// as the output package's blocks through the reporter's Show, each block
// colored only when the run's color is on. A captioned block follows a
// plain Note of its caption. An empty Note prints a blank line, which sets
// a block off: an SQL, JSON, or table block has one before its caption and
// one after it, and a request, its response, and the trace pointer read
// off them each have one before, so they read apart.
//
// The wire types and routes come from domain/organization, domain/document,
// and admin/database, which restate the service's contract once for every
// slab command. The storage, events, and sweeper scenarios wait for the
// sweep through document.AwaitSweep, the one definition of dirs delete's
// --wait. A step's method name is its Intent in lowerCamel, with articles and
// prepositions dropped and a nominalized title turned back into its verb
// ("Catalog registration" becomes registerCatalog).
//
// A scenario's steps hand forward what they read in one value, each part
// replaced by the step that reads it first, the client by the first step,
// so nothing a run reads is left from a run before it. A read a step makes
// for bookkeeping, such as the ids and versions a later command guards on,
// is not narrated. The domain and problems scenarios start by resetting to
// the seeded state, so a fixed code or version is safe to target: the
// domain scenario writes no row twice, so each If-Match carries the version
// where its row was last seen, and no problems condition writes a row at
// all.
//
// A need node's probe that the run context ended during, by a signal or by
// another need's failed start in the same layer, returns the context's
// error, which the lifecycle drops once a failure is on record, so the
// cancelled probe adds no second report naming a task that would fix
// nothing.
//
// Some values are restated from the service, which slab does not import:
// the service.name it exports (internal/app/telemetry.go and
// compose/otel-collector/otel-collector.yaml state it too), the curated
// detail of a taken name's 409 (data.DetailNameTaken), the sweeper's
// reclaimers and outcomes (internal/app's reactors.go and telemetry.go),
// and the command body limit (domain/organization/handler.go), a fixture
// value for the problems scenario rather than a restated contract. Its
// absent id is the one integration/organization_test.go uses: valid UUID
// syntax, never minted.
//
// A wait for the sweep is bounded at 45 seconds: the delete nudges the
// sweep, so a small branch is gone well inside it, and the sweep's own
// 30-second interval is the fallback the bound leaves room for. The sweeper
// scenario waits for an export up to two of the service's metric export
// intervals, the OpenTelemetry SDK's 60-second default, which the service,
// the collector, and the configuration leave unset, so the export in
// flight and the next both fit, plus half a minute for the collector's
// batch and Mimir's ingest. It
// reads the sweep block as config.local.json merges over config.json
// (mise run serve runs with APP_ENV=local), without the APP_SWEEP_*
// overrides of the service's environment, which slab cannot see. Its
// narration claims no more than the counts show: go-database's
// sweep.Sweeper runs every reclaimer that is due at each delivery, a tick
// or a nudge, counting one due once its interval less half a tick has
// passed (Sweeper.due), so when files ran no more passes than either of the
// others the counts cannot tell the nudged pass from a tick.
package scenarios
