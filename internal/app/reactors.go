package app

import (
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-messaging"
	msgpg "github.com/standards-lab/go-messaging/postgres"
)

// defineReactors defines the reactors on g into n: the entry points that
// run for the process lifetime, driven by an occurrence rather than a
// caller. The sweep is one: its wake, and the sweeper that receives from
// it. The messaging is the other: the runtime, and the relay it builds,
// which publishes the outbox's committed events. No node Uses a reactor,
// so each is listed in n.Reactors, which Run builds as roots and the
// server orders itself after. It constructs nothing.
//
// The relay's last pass publishes what was committed while the producers
// drained, so every producer must drain before it. A node that commits
// events outside a request, such as a consumer's reactor, Uses the relay's
// node, which orders it above the relay. A request-driven producer, as
// every domain service is, needs no edge: its commands run in the server's
// requests, and the server, ordered after every reactor, drains first.
func defineReactors(g *graph.Graph, n *Nodes) {
	n.Wake = g.Define("wake", newWake(n))
	n.Sweeper = g.Define("sweeper", newSweeper(n))
	n.Runtime = g.Define("runtime", newRuntime(n))
	n.Relay = g.Define("relay", newRelay(n))
	n.Reactors = []graph.Ref{n.Sweeper, n.Relay}
}

// Wake is the wake node's value, where the sweep's two halves meet: the
// document layer nudges it after each branch it marks, so it is that
// layer's document.Sweeper, and the sweeper receives from its source. It
// has no Ready method: the source's own readiness is the sweeper's to
// report, so the probe gains no "wake" check.
type Wake struct {
	source *reactor.Waker
}

// Nudge asks the sweeper for a pass as soon as it is free.
func (w *Wake) Nudge() { w.source.Nudge() }

// newWake constructs the sweep's source from the config node's sweep
// block. The interval is the backstop for a nudge that never came and the
// schedule of the stale rows' reclaim. The wake is nudged once here, which
// the source delivers as soon as it starts, so a branch marked by a process
// that stopped before its sweep ran is swept at this process's start rather
// than an interval later.
func newWake(n *Nodes) func(*graph.Scope) (*Wake, error) {
	return func(s *graph.Scope) (*Wake, error) {
		w := &Wake{source: reactor.Wake(s.Use(n.Config).Sweep.Interval.Duration())}
		w.Nudge()
		return w, nil
	}
}

// newSweeper constructs the sweep's reactor over the wake's source, the
// files node's sweep worker, and the gate. Its value is a lifecycle
// Subsystem, ReadinessChecker, and Monitored, so the Coordinator starts it,
// drains it, reports it, and ends the run on its failure with no
// registration here. It orders itself after the schema, so it starts once
// every table it touches is verified, and stops before the schema's layer
// beneath it. Its reactor.CancelWithin option cancels a pass still running
// at half the shutdown timeout, so the sweeper reports the cancellation
// before the coordinator's deadline drops it.
func newSweeper(n *Nodes) func(*graph.Scope) (*reactor.Reactor[time.Time], error) {
	return func(s *graph.Scope) (*reactor.Reactor[time.Time], error) {
		s.After(n.Schema)
		cfg := s.Use(n.Config)

		sweep := s.Use(n.Files).SweepWorker(s.Use(n.SQL).DB, s.Use(n.Gate), s.Use(n.Logger),
			bfdata.Batch(*cfg.Sweep.Batch),
			bfdata.StaleOlderThan(cfg.Sweep.StaleAge.Duration()),
		)
		return reactor.New(s.Use(n.Wake).source, sweep,
			reactor.CancelWithin(cfg.ShutdownTimeout.Duration())), nil
	}
}

// newRuntime constructs the service's messaging from the config node's
// messaging block, over the broker node and go-messaging's Postgres engine
// statements, logging failures through the logger node. Its reactors drain
// within the shutdown timeout and hold the gate shared, so a schema verb,
// which holds it exclusively, runs with them paused: the relay opens no
// transaction and claims no row until the verb releases it. The runtime
// holds no resource and registers nothing, so it is no lifecycle
// participant; the broker beneath it and the reactors it builds are.
func newRuntime(n *Nodes) func(*graph.Scope) (*messaging.Runtime, error) {
	return func(s *graph.Scope) (*messaging.Runtime, error) {
		cfg := s.Use(n.Config)
		return messaging.New(cfg.Messaging, s.Use(n.Broker), msgpg.Outbox(), msgpg.Inbox(),
			cfg.ShutdownTimeout.Duration(), s.Use(n.Logger), messaging.Gated(s.Use(n.Gate)))
	}
}

// newRelay constructs the relay over the sql node's session. Its value is a
// lifecycle Subsystem, ReadinessChecker, and Monitored, as the sweeper's
// is. It orders itself after the schema, so its first pass finds the
// messaging set migrated and verified. The runtime gives it go-messaging's
// drain: a last pass bounded at a quarter of the shutdown timeout, and the
// reactor's cancellation of a publish still running at half of it. It drains
// before the broker beneath it, which it Uses through the runtime, so the
// last pass publishes before the connection closes.
func newRelay(n *Nodes) func(*graph.Scope) (*reactor.Reactor[event.Event], error) {
	return func(s *graph.Scope) (*reactor.Reactor[event.Event], error) {
		s.After(n.Schema)
		return s.Use(n.Runtime).Relay(s.Use(n.SQL).DB), nil
	}
}
