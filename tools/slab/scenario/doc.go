// Package scenario defines what a slab scenario is, the reporter it narrates
// through, the cobra command that runs one, and the listing that prints them.
// There is no registry: [Command] builds one cobra command from a scenario a
// caller hands it, the same way a domain package's Commands is called.
//
// The package exports:
//
//   - [Scenario], an ordered list of [Step]s with the [Need]s checked before
//     the first
//   - [Reporter] and [NewReporter], the observation channels a step reports
//     through
//   - [Run], which runs a scenario through a reporter
//   - [Command], the cobra command that runs one scenario
//   - [WriteListing], which prints the scenarios and their needs
package scenario
