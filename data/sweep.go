package data

import (
	"context"
	"log/slog"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/sdk"
)

// The sweep worker is the storage infrastructure's own upkeep: it finishes
// the deletes the domains begin (a branch a recursive delete marks) and
// reclaims the stale rows a write or a delete that stopped partway leaves.
// A Reactor, in the architecture's sense, dispatches an occurrence to a
// Domain Service; the sweep worker calls blobfs and the object store,
// never a domain service, so it is not one. It is infrastructure work that
// needs a process-lifetime runner, and the composition root stages it on
// the sdk reactor with a Wake source, since the reactor is the one runner
// the process has. The worker is written in two halves so each can move on
// its own: sweepUntilDone, the loop over blobfs's bounded passes, staged
// for promotion to blobfs beside Sweep; and the logging policy, which is
// this service's.
//
// It is the standalone sweeper of blobfs's deletes, the one reclamation
// the service runs. The moment any other layer needs sweeper-like
// reclamation (outbox cleanup, expired sessions, soft-delete purge, or any
// cascade beyond pruning SQL rows), that step builds the general sweeper
// and moves blobfs's sweep onto it as its first reclaimer; it never builds
// a second standalone worker.

// sweepPass is one bounded pass of blobfs's sweep with the service's
// options, the unit the worker repeats.
type sweepPass func(ctx context.Context) (bfdata.SweepResult, error)

// SweepGate is what the worker asks of the process before each pass: its
// turn, alongside any other background work, while no schema change runs.
// Shared waits out a schema change that holds the gate or waits for it,
// and returns the release, or ctx's error, holding nothing, if ctx ends
// first. The worker declares it and the composition root injects it; the
// process's quiesce gate satisfies it, and the database admin domain holds
// the same gate exclusively around every verb that changes the schema.
//
// A pass must not run under a schema change: a pass's directory removal
// cascades from blobfs_directory into organization_directory, and a
// revert that drops that table or alters its foreign key locks the two in
// the opposite order, so the two deadlock and Postgres aborts one; a pass
// that runs while the tables are dropped fails on every statement. The
// gate is per process, so it orders this process's sweep against this
// process's schema changes only; the database admin domain's SchemaGate
// says what that leaves out.
type SweepGate interface {
	Shared(ctx context.Context) (release func(), err error)
}

// SweepWorker returns the sweep worker as a reactor's Func. On each wake
// it runs blobfs's sweep over db and the object store, with opts, in
// passes while a pass reports More, each pass holding gate shared, and
// logs each pass to logger. One wake finishes the work waiting, in passes
// of the configured batch, and the next wake finds whatever arrived since.
// Once the reactor's drain begins (sdk.Draining), the worker runs no
// further pass: the pass in flight finishes and the worker returns nil,
// so a large backlog never holds a drain until the grace cancels it. What
// remains is found in the database at the next start's wake.
//
// A pass's error never fails the worker. blobfs returns a pass's
// persistent refusals joined (an object delete the store refused, a purge
// a foreign key refused), leaves each refused row for a later pass, and
// sets no More for them; a read that fails ends the pass with its error
// the same way. The two are not told apart, and need not be: every step
// of the sweep is idempotent and the work is found in the database on each
// pass, so the next wake retries all of it, and the sweep has nothing to
// redeliver. The worker logs the error at warn with the pass's counts and
// returns nil, the spike's rule for a handler that tolerates a failure. A
// stuck row, a database or object-store outage, or an admin state reset
// under a running pass therefore never ends the process; an outage shows
// on the database's and the store's own readiness checks instead. The one error
// it returns is its context's, once the drain cancels it past the
// reactor's Grace while a pass is in flight or waits on the gate, which
// Shutdown reports as handlers cancelled.
func (s *Storage) SweepWorker(db *sqlate.DB, gate SweepGate, logger *slog.Logger, opts ...bfdata.SweepOption) sdk.Func[time.Time] {
	return sweepWorker(gated(gate, func(ctx context.Context) (bfdata.SweepResult, error) {
		return s.FS.Sweep(ctx, db, s.Objects, opts...)
	}), logger)
}

// gated runs pass holding gate shared, so a pass waits out a schema change
// and a schema change waits for the pass in flight. A wait that ctx ends
// runs no pass and returns ctx's error, which ends the worker's loop as
// the drain's cancellation.
func gated(gate SweepGate, pass sweepPass) sweepPass {
	return func(ctx context.Context) (bfdata.SweepResult, error) {
		release, err := gate.Shared(ctx)
		if err != nil {
			return bfdata.SweepResult{}, err
		}
		defer release()
		return pass(ctx)
	}
}

// sweepWorker joins the loop to the logging policy over any pass, and
// hands the loop the reactor's drain signal as its stop.
func sweepWorker(pass sweepPass, logger *slog.Logger) sdk.Func[time.Time] {
	report := logPass(logger)
	return func(ctx context.Context, _ time.Time) error {
		return sweepUntilDone(ctx, sdk.Draining(ctx), pass, report)
	}
}

// sweepUntilDone runs pass while it reports More, checking ctx and stop
// before each, and hands every pass's result and error to report. It
// returns nil once a pass reports no More or once stop is closed, which
// ends the loop between passes and never interrupts one, and ctx's error
// once ctx ends, before a pass or during one; a pass's own error is
// report's to judge and never ends the loop. A nil stop never closes. It
// knows nothing of this service or the reactor, and is the half staged
// for blobfs.
func sweepUntilDone(ctx context.Context, stop <-chan struct{}, pass sweepPass, report func(bfdata.SweepResult, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-stop:
			return nil
		default:
		}
		res, err := pass(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		report(res, err)
		if !res.More {
			return nil
		}
	}
}

// logPass is the worker's logging policy: a pass that returned refusals
// at warn with its error and counts, a pass that did work at info, and a
// pass with nothing to do not at all.
func logPass(logger *slog.Logger) func(bfdata.SweepResult, error) {
	return func(res bfdata.SweepResult, err error) {
		counts := []any{"files", res.Files, "directories", res.Directories, "stale", res.Stale, "more", res.More}
		switch {
		case err != nil:
			logger.Warn("sweep pass refused", append(counts, "error", err)...)
		case res.Files+res.Directories+res.Stale > 0:
			logger.Info("sweep pass", counts...)
		}
	}
}
