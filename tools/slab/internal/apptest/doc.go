// Package apptest holds the fixtures internal/app's tests share for running
// the program over the composition [app.App] publishes: its graph and its
// nodes. A test builds its App with [app.New] and applies the fixtures to
// it before it runs, each an observer or a Replace on the App's graph; a
// constructor a fixture does not swap stays the production one.
//
// [Builds] records the name of every node a run's Build begins, through
// graph.Graph.Observe, so a test asserts which nodes a command builds from
// what the graph reports. [Halt] fails the lifecycle configuration node,
// which the dispatcher adds to every Build, so a run of a command that
// declares nodes fails before anything starts.
//
// The package exports:
//
//   - [Recorder], the events a test records, in order, and its
//     [Recorder.Record] and [Recorder.Log]
//   - [Builds], which returns a Recorder of the nodes the App's runs build
//   - [Halt], which stops the App's runs before anything starts, and
//     [ErrHalted], what such a run fails with
package apptest
