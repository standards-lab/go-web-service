// Package demo holds the narrated scenarios: sqlate, which runs a library's
// mechanism in process over the service's own sources on disk with nothing
// running; domain, which runs the organization domain's full CRUD surface
// against the running service, reseeded from a known fixture each run; and
// problems, which sends the running service one request per error
// condition it answers with a problem document. Each file is one scenario:
// its literal and the step methods that run it. calls.go holds the calls
// and checks two scenarios make identically; the wire types and routes they
// send come from domain/organization and admin/database, which restate the
// service's contract once for every slab command.
//
// A scenario file exports its constructor (Compile, Organization, Problems)
// and registers nothing: commands.go holds Scenarios, the explicit ordered
// list, and Commands, the demo subtree built from it, which panics on a
// duplicate name. A call goes in calls.go only when two scenarios make it
// identically; a step whose point is showing its own literal request keeps
// that request in its scenario file. A step's method name is its Intent in
// lowerCamel, articles and prepositions dropped and a nominalized title
// turned back into its verb ("Catalog registration" becomes
// registerCatalog).
package demo
