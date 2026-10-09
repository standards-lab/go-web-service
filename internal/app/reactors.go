package app

import (
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/sdk"
)

// defineReactors defines the reactors on g into n: the entry points that
// run for the process lifetime, driven by an occurrence rather than a
// caller. The sweep is the only one: its wake, and the sweeper that
// receives from it. No node Uses a reactor, so the sweeper is listed in
// n.Reactors, which Run builds as roots and the server orders itself
// after. It constructs nothing.
func defineReactors(g *graph.Graph, n *Nodes) {
	n.Wake = g.Define("wake", newWake(n))
	n.Sweeper = g.Define("sweeper", newSweeper(n))
	n.Reactors = []graph.Ref{n.Sweeper}
}

// Wake is the wake node's value, where the sweep's two halves meet: the
// document layer nudges it after each branch it marks, so it is that
// layer's document.Sweeper, and the sweeper receives from its source. It
// deliberately has no Ready method: the lifecycle infers a readiness check
// from any node value's Ready, and the source's own readiness is the
// sweeper's to report, so the probe gains no "wake" check.
type Wake struct {
	source *sdk.Waker
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
		w := &Wake{source: sdk.Wake(s.Use(n.Config).Sweep.Interval.Duration())}
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
// beneath it.
//
// Its Grace is half the shutdown timeout: the drain stops the server first
// under the one timeout, so the server keeps its share, and a reactor that
// had to cancel its handlers says so before the coordinator's deadline
// drops the report.
func newSweeper(n *Nodes) func(*graph.Scope) (*sdk.Reactor[time.Time], error) {
	return func(s *graph.Scope) (*sdk.Reactor[time.Time], error) {
		s.After(n.Schema)
		cfg := s.Use(n.Config)
		grace := sdk.Grace(cfg.ShutdownTimeout.Duration() / 2)

		sweep := s.Use(n.Files).SweepWorker(s.Use(n.SQL).DB, s.Use(n.Gate), s.Use(n.Logger),
			bfdata.Batch(*cfg.Sweep.Batch),
			bfdata.StaleOlderThan(cfg.Sweep.StaleAge.Duration()),
		)
		return sdk.New(s.Use(n.Wake).source, sweep, grace), nil
	}
}
