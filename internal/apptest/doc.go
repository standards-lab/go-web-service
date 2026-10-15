// Package apptest holds the fixtures internal/app's tests share for
// exercising the composition [app.App] publishes: its graph and its nodes.
// A test builds its App with [app.New] and applies the fixtures to it
// before it builds, each a Replace on the App's graph or a Build of it; a
// constructor a fixture does not swap stays the production one, and no
// fixture wraps one.
//
// [ManualMeter] replaces the meter provider node's constructor with an
// OpenTelemetry SDK provider over a manual reader, so a test collects the
// service's own instruments on demand rather than through the OTLP
// exporter. [SweepHook] builds the sweep metrics node and returns its
// value, the sweeper's per-pass hook, so a test reports passes to it
// directly.
//
// The package exports:
//
//   - [ManualMeter], which substitutes the meter provider node and returns
//     the reader a test collects from
//   - [SweepHook], the hook the App's sweep metrics node builds
package apptest
