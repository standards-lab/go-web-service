// Package demo holds slab's narrated scenarios. The sqlate scenario runs a
// library's mechanism in process over the service's own sources on disk,
// with nothing running. The domain scenario runs the organization domain's
// full CRUD surface against the running service, reseeded from a known
// fixture on each run. The problems scenario sends the running service one
// request per error condition it answers with a problem document.
//
// Each file holds one scenario: its literal and the step methods that run
// it. A scenario file exports its constructor (Compile, Organization,
// Problems) and registers nothing: commands.go holds Scenarios, the explicit
// ordered list, and Commands, which builds the demo subtree from that list
// and panics on a duplicate name. calls.go holds a call or check only when
// two scenarios make it identically; a step whose point is showing its own
// literal request keeps that request in its scenario file. The wire types
// and routes come from domain/organization and admin/database, which
// restate the service's contract once for every slab command.
//
// A step's method name is its Intent in lowerCamel, with articles and
// prepositions dropped and a nominalized title turned back into its verb
// ("Catalog registration" becomes registerCatalog).
package demo
