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
// its organization's document root and then the organization.
//
// The four scenarios against the running service declare the service
// node, whose value is a [Service]; the two that point at traces declare
// the grafana up node too, whose value is a [Grafana]. Each value is a
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
//   - [Sqlate], [Domain], [Problems], [Storage], and [Events], the scenarios
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
// slab command. The storage and events scenarios wait for the sweep through
// document.AwaitSweep, the one definition of dirs delete's --wait. A
// step's method name is its Intent in lowerCamel, with articles and
// prepositions dropped and a nominalized title turned back into its verb
// ("Catalog registration" becomes registerCatalog).
package scenarios
