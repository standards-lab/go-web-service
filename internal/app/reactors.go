package app

import (
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/sdk"
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a domain
// service call, the inbound counterpart to a route. The messaging layer
// adds the subscriptions. Sweep is the one the service runs, and the
// exception to that definition: it runs the data package's sweep worker,
// blobfs's sweep, and calls no domain service. It is staged here because the
// reactor is the process's one runner for work that lasts the process
// lifetime, woken by an interval and by the document layer's nudge.
type Reactors struct {
	Sweep *sdk.Reactor[time.Time]
}

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

// newReactors constructs the reactors and registers each on lc at
// stageReactors. It takes infra for the connections a reactor owns, cfg
// for its schedule and its options, wake for the source the sweep
// receives from, and gate for the quiesce gate each sweep pass holds
// shared, so no pass runs under an admin verb that changes the schema.
// Each reactor runs for the process lifetime, so it registers on the
// coordinator as an infrastructure service does, and lc monitors its Err,
// so a reactor that fails ends the process. Every
// reactor's Grace is half the shutdown timeout: the drain runs the root
// stage first under the one timeout, so the server keeps its share, and a
// reactor that had to cancel its handlers says so before the
// coordinator's deadline drops the report.
func newReactors(
	infra *Infrastructure,
	cfg *config.Config,
	wake *sdk.Waker,
	gate *sdk.Gate,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
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

	return &Reactors{Sweep: sweeper}, nil
}
