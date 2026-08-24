// Package reactors is the composition root for the application's
// event-driven entry points: components that watch a source of occurrences
// (a subscription, a poll interval, a schedule) and dispatch each one to a
// domain service call. Routes are the counterpart, entry points driven by an
// external caller; a reactor is driven by an occurrence it either receives
// or discovers. The template ships no reactor; an application built from the
// template adds fields to Reactors and constructs and registers them in
// [New].
//
// A reactor owns a transport connection and runs for the process lifetime,
// so [New] registers each one on the coordinator, at whatever stage its
// transport's readiness requires.
package reactors
