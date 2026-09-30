package app

import (
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/sdk"
)

// newSweepWake constructs the sweep's source ahead of the domain, since
// the two halves of the sweep meet in it: the document layer nudges it
// after each branch it marks, so it is that layer's document.Sweeper, and
// the sweep's reactor receives from it. The interval is the backstop
// for a nudge that never came and the schedule of the stale rows' reclaim.
// The wake is nudged once here, which the source delivers as soon as it
// starts, so a branch marked by a process that stopped before its sweep
// ran is swept at this process's start rather than an interval later.
func newSweepWake(cfg *config.Config) *sdk.Waker {
	wake := sdk.Wake(cfg.Sweep.Interval.Duration())
	wake.Nudge()
	return wake
}

// newReactors constructs the reactors, the sweep the one so far, over
// wake and gate, and registers each on lc at stageReactors with its Err
// monitored, so a reactor that fails ends the process. Every reactor's
// Grace is half the shutdown timeout: the drain runs the root stage first
// under the one timeout, so the server keeps its share, and a reactor that
// had to cancel its handlers says so before the coordinator's deadline
// drops the report.
func newReactors(
	infra *Infrastructure,
	cfg *config.Config,
	wake *sdk.Waker,
	gate *sdk.Gate,
	lc *lifecycle.Coordinator,
) {
	grace := sdk.Grace(cfg.ShutdownTimeout.Duration() / 2)

	sweep := infra.Storage.SweepWorker(infra.SQL.DB, gate, infra.Logger,
		bfdata.Batch(*cfg.Sweep.Batch),
		bfdata.StaleOlderThan(cfg.Sweep.StaleAge.Duration()),
	)
	sweeper := sdk.New(wake, sweep, grace)
	lc.Add(lifecycle.Service{
		Name:     "sweeper",
		Stage:    stageReactors,
		Start:    sweeper.Start,
		Shutdown: sweeper.Shutdown,
		Check:    sweeper,
	})
	lc.Monitor(sweeper.Err())
}
