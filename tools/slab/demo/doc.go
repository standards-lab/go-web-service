// Package demo holds slab's narrated scenarios. The sqlate scenario runs a
// library's mechanism in process over the service's own sources on disk,
// with nothing running. The domain scenario runs the organization domain's
// full CRUD surface against the running service, reseeded from a known
// fixture on each run. The problems scenario sends the running service one
// request per error condition it answers with a problem document. The
// storage scenario walks the service's stored files: the migration sets, a
// logo's replace cycle, a document tree and a cursor walk, the scoped
// refusals, a recursive delete waited out to 404, and a reset. It starts
// with an additive seed rather than a reset, and names what it creates for
// the run, so it runs twice in a row without a reset between.
//
// The package exports:
//
//   - [Compile], [Organization], [Problems], and [Storage], the scenarios
//   - [Scenarios], the ordered list, and [Commands], the demo subtree
//   - [SeedState], [SeedsDir], and [FixturesDir], the seeded state and where
//     the service's states and fixtures live
//   - [ServiceName], the service name a trace is indexed under
//   - [Reset] and [List], the narrated calls two scenarios make identically
//   - [Tree], the seeded organizations by code
//   - [IdentityOf] and [ExpectVersion], the checks on a command's reply
//   - [OversizedBody], a command body over the service's limit
//
// Each file holds one scenario: its literal and the step methods that run
// it. A scenario file exports its constructor (Compile, Organization,
// Problems, Storage) and registers nothing: commands.go holds Scenarios,
// the explicit ordered list, and Commands, which builds the demo subtree
// from that list. calls.go holds a call or check only when
// two scenarios make it identically; a step whose point is showing its own
// literal request keeps that request in its scenario file. The wire types
// and routes come from domain/organization, domain/document, and
// admin/database, which restate the service's contract once for every slab
// command. The storage scenario waits for the sweep through
// document.AwaitSweep, the one definition of dirs delete's --wait.
//
// A step's method name is its Intent in lowerCamel, with articles and
// prepositions dropped and a nominalized title turned back into its verb
// ("Catalog registration" becomes registerCatalog).
package demo
