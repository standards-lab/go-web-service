// Command slab calls the service's endpoints, one subcommand per endpoint,
// and runs its narrated scenarios, each printing what it is about to do and
// what it observed.
//
// The first signal cancels the run's context and restores the default
// handlers, so a second signal terminates the process even if a scenario
// ignores the cancellation. The signal registration is released before
// os.Exit, which skips deferred calls.
package main
