package app

import (
	"context"
	"log/slog"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/sdk"
)

// Reactors composes the application's event-driven entry points: components
// that watch a source of occurrences and dispatch each one to a domain
// service call, the inbound counterpart to a route. Sweep is the one the
// service runs: blobfs's sweep, woken by an interval and by the document
// layer's nudge. The messaging layer adds the subscriptions.
type Reactors struct {
	Sweep *sdk.Reactor[time.Time]
}

// sweepStage places the sweep reactor after the domains, whose statements
// and rows it depends on, and below the root: it starts once every table
// it sweeps is verified, and the drain stops it after the server and
// before the domains and the pool beneath it.
const sweepStage = document.Stage + 1

// newSweepWake constructs the sweep reactor's source ahead of the domain,
// since the two halves of the sweep meet in it: the document layer nudges
// it after each branch it marks, so it is that layer's document.Sweeper,
// and the sweep reactor receives from it. The interval is the backstop
// for a nudge that never came, a branch marked by a process that stopped
// before its sweep ran, and the schedule of the stale rows' reclaim.
func newSweepWake(cfg *config.Config) *sdk.Waker {
	return sdk.Wake(cfg.Sweep.Interval.Duration())
}

// newReactors constructs the reactors and registers each on lc. It takes
// infra for the connections a reactor owns, cfg for its schedule, and wake
// for the source the sweep receives from. The sweep dispatches to no
// domain call: a document root's owner row goes with its directory through
// the row's cascading foreign key, so the sweep needs no removal hook.
// Each reactor runs for the process lifetime, so it registers on the
// coordinator as an infrastructure service does, and lc monitors its Err,
// so a reactor that fails ends the process. Every reactor's Grace is
// half the shutdown timeout: the drain runs the root stage first under the
// one timeout, so the server keeps its share, and a reactor that had to
// cancel its handlers says so before the coordinator's deadline drops the
// report.
func newReactors(
	infra *Infrastructure,
	cfg *config.Config,
	wake *sdk.Waker,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	grace := sdk.Grace(cfg.ShutdownTimeout.Duration() / 2)

	opts := []bfdata.SweepOption{
		bfdata.Batch(*cfg.Sweep.Batch),
		bfdata.StaleOlderThan(cfg.Sweep.StaleAge.Duration()),
	}
	pass := func(ctx context.Context) (bfdata.SweepResult, error) {
		return infra.Storage.FS.Sweep(ctx, infra.SQL.DB, infra.Storage.Objects, opts...)
	}
	sweeper := sdk.New(wake, sweep(pass, infra.Logger), grace)
	lc.Add(lifecycle.Service{
		Name:     "sweeper",
		Stage:    sweepStage,
		Start:    sweeper.Start,
		Shutdown: sweeper.Shutdown,
		Check:    sweeper,
	})
	lc.Monitor(sweeper.Err())

	return &Reactors{Sweep: sweeper}, nil
}

// sweepPass is one bounded pass of blobfs's sweep with the service's
// options, the unit the sweep reactor repeats.
type sweepPass func(ctx context.Context) (bfdata.SweepResult, error)

// sweep is the sweep reactor's Func: on each wake it runs passes while a
// pass reports More, checking ctx before each, so one wake finishes the
// work waiting in passes of the configured batch, and the next wake
// finds whatever arrived since. It is the standalone sweeper of blobfs's
// deletes, the one reclamation the service runs. The moment any other
// layer needs sweeper-like reclamation (outbox cleanup, expired sessions,
// soft-delete purge, or any cascade beyond pruning SQL rows), that step
// builds the general sweeper and moves blobfs's sweep onto it as its
// first reclaimer; it never builds a second standalone reactor.
//
// A pass's error never fails the reactor. blobfs returns a pass's
// persistent refusals joined (an object delete the store refused, a
// purge a foreign key refused), leaves each refused row
// for a later pass, and sets no More for them; a read that fails ends the
// pass with its error the same way. The two are not told apart, and need
// not be: every step of the sweep is idempotent and the work is found in
// the database on each pass, so the next wake retries all of it, and the
// sweep has nothing to redeliver. The Func logs the error at warn with
// the pass's counts and returns nil, the spike's rule for a handler that
// tolerates a failure, so a stuck row, a database or object-store outage,
// or an admin state reset under a running pass never ends the process;
// the outage shows on the database's and the store's own readiness
// checks instead. The one error it returns is its context's, once the
// drain cancels it past the reactor's Grace, which Shutdown reports as
// handlers cancelled.
func sweep(pass sweepPass, logger *slog.Logger) sdk.Func[time.Time] {
	return func(ctx context.Context, _ time.Time) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			res, err := pass(ctx)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			counts := []any{"files", res.Files, "directories", res.Directories, "stale", res.Stale, "more", res.More}
			switch {
			case err != nil:
				logger.Warn("sweep pass refused", append(counts, "error", err)...)
			case res.Files+res.Directories+res.Stale > 0:
				logger.Info("sweep pass", counts...)
			}
			if !res.More {
				return nil
			}
		}
	}
}
