// Package scenarios holds slab's narrated scenarios on go-cli-sdk's
// scenario runner, each run as "slab scenario <name>". The package is
// built over the node handles the composition root hands it, in [Nodes]:
// each scenario declares the nodes it reads, and its steps read their
// values from the invocation, so a scenario brings up only what it needs.
// The sqlate scenario runs a library's mechanism in process over the
// service's own sources on disk, with nothing running.
//
// The package exports:
//
//   - [Nodes], the graph nodes the scenarios are built over
//   - [All], the scenarios in presentation order
//   - [Sqlate], the sqlate scenario
//
// scenarios.go holds Nodes, All, and what every scenario's steps share;
// each other file holds one scenario: its constructor, its literal, and
// the step methods that run it. A step narrates its prose with the
// reporter's Note and shows its results as the output package's blocks
// through the reporter's Show, each block captioned by a plain Note before
// it and colored only when the run's color is on. A step's method name is
// its Intent in lowerCamel, with articles and prepositions dropped and a
// nominalized title turned back into its verb ("Catalog registration"
// becomes registerCatalog).
package scenarios
