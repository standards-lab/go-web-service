package data

import (
	"context"
	"log/slog"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate"
)

// FilesReclaimer is blobfs's sweep as one reclaimer of go-database's
// general sweeper (package sweep), which it satisfies structurally: each
// Pass is one bounded pass of the store's sweep over the database and the
// object store, finishing what a recursive delete marked and reclaiming
// the stale file rows. The sweeper decides when a pass runs, holds the
// process's quiesce gate around it, and claims the reclaimer's lock; the
// reclaimer owns only the pass and its counts.
type FilesReclaimer struct {
	storage  *Storage
	db       *sqlate.DB
	staleAge time.Duration
	logger   *slog.Logger
}

// FilesReclaimer returns blobfs's sweep over db and the object store as a
// reclaimer, each pass reclaiming the pending and deleting file rows older
// than staleAge (blobfs's StaleOlderThan) and logging its counts to logger.
func (s *Storage) FilesReclaimer(db *sqlate.DB, staleAge time.Duration, logger *slog.Logger) *FilesReclaimer {
	return &FilesReclaimer{storage: s, db: db, staleAge: staleAge, logger: logger}
}

// Pass runs one pass of blobfs's sweep handling at most budget records
// (blobfs's Batch), and reports the pass's More and its error, which
// gathers the refusals the pass met. Every step of the sweep is idempotent
// and its work is found in the database, so a refused record is left for a
// later pass, and the sweeper reports the error at warn.
//
// A pass that did work or met refusals is logged at info as "sweep pass"
// with its counts: the files, directories, and stale rows it removed, and
// whether it reported More, so a refused pass logs its counts even when
// they are all zero. Only a pass with nothing to do and no refusal is not
// logged. The error itself is left to the sweeper's report, at warn, so a
// refusal is logged once at warn and its counts once at info.
func (r *FilesReclaimer) Pass(ctx context.Context, budget int) (bool, error) {
	res, err := r.storage.FS.Sweep(ctx, r.db, r.storage.Objects,
		bfdata.Batch(budget), bfdata.StaleOlderThan(r.staleAge))
	if err != nil || res.Files+res.Directories+res.Stale > 0 {
		r.logger.InfoContext(ctx, "sweep pass",
			"files", res.Files, "directories", res.Directories, "stale", res.Stale, "more", res.More)
	}
	return res.More, err
}
