package data

import (
	"context"
	"log/slog"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate"

	"github.com/standards-lab/go-web-service/sdk"
)

// sweepPass is one bounded pass of blobfs's sweep with the service's
// options, the unit bfdata.SweepUntilDone repeats.
type sweepPass func(ctx context.Context) (bfdata.SweepResult, error)

// SweepGate is what the worker asks of the process before each pass: its
// turn while no schema change runs. Shared waits out a schema change that
// holds the gate or waits for it and returns the release, or returns ctx's
// error, holding nothing, if ctx ends first. The composition root injects
// the process's quiesce gate, which the database admin domain holds
// exclusively around every verb that changes the schema; its SchemaGate
// says why a pass must not run under one.
type SweepGate interface {
	Shared(ctx context.Context) (release func(), err error)
}

// SweepWorker returns the sweep as a reactor's Func: on each wake it runs
// blobfs's sweep over db and the object store with opts, in passes while a
// pass reports More, each pass holding gate shared and logged to logger.
// Once the reactor's drain begins (sdk.Draining) it runs no further pass,
// so a backlog never holds a drain; the next start's wake finds the rest.
//
// A pass's error never fails the worker: every step of the sweep is
// idempotent and its work is found in the database, so the worker logs the
// error at warn with the pass's counts and the next wake retries. An
// outage shows on the database's and the store's readiness checks instead.
// The one error it returns is its context's, once the drain cancels a pass
// in flight or a wait on the gate past the reactor's Grace.
func (s *Storage) SweepWorker(db *sqlate.DB, gate SweepGate, logger *slog.Logger, opts ...bfdata.SweepOption) sdk.Func[time.Time] {
	pass := gated(gate, func(ctx context.Context) (bfdata.SweepResult, error) {
		return s.FS.Sweep(ctx, db, s.Objects, opts...)
	})
	report := logPass(logger)
	return func(ctx context.Context, _ time.Time) error {
		return bfdata.SweepUntilDone(ctx, sdk.Draining(ctx), pass, report)
	}
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
