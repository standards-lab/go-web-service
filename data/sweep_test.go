package data_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/blobfs/data/datatest"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
)

// The files reclaimer is one of go-database's sweeper's, structurally.
var _ sweep.Reclaimer = (*data.FilesReclaimer)(nil)

// logBuffer serializes the reclaimer's log writes and the test's reads.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logged is a logger over a buffer the test reads.
func logged() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

// staleAge is the reclaimer's stale age in the scripts, whose stale rows
// are older.
const staleAge = time.Hour

// noRoots scripts a read of the deleting branches' roots that finds none.
func noRoots() sqltest.Response { return datatest.DirectoryRows() }

// stale is a deleting file row past the stale age, one per id.
func stale(id string) sqltest.Response {
	f := file(blobfs.StatusDeleting, 3)
	f.ID, f.Key = id, id+"/report.txt"
	f.UpdatedAt = f.UpdatedAt.Add(-2 * time.Hour)
	return datatest.FileRows(f)
}

const (
	stale1 = "00000000-0000-7000-8000-000000000001"
	stale2 = "00000000-0000-7000-8000-000000000002"
	stale3 = "00000000-0000-7000-8000-000000000003"
)

// morePass scripts a pass that reclaims the stale row with id, spends the
// batch, and finds next beyond it: Stale 1, More.
func morePass(id, next string) []sqltest.Response {
	return []sqltest.Response{noRoots(), stale(id), exec(1), noRoots(), stale(next)}
}

// lastPass scripts a pass that reclaims the stale row with id and finds
// nothing beyond it: Stale 1, no More.
func lastPass(id string) []sqltest.Response {
	return []sqltest.Response{noRoots(), stale(id), exec(1), noRoots(), datatest.FileRows()}
}

// idlePass scripts a pass with nothing to do.
func idlePass() []sqltest.Response { return []sqltest.Response{noRoots(), datatest.FileRows()} }

var (
	moreOps = []sqltest.Op{q, q, x, q, q}
	idleOps = []sqltest.Op{q, q}
)

// A pass that did work and found more beyond its budget reports more, and
// is logged at info with its counts. The budget is the pass's batch: a
// budget of one reclaims one stale row and reads the next as more.
func TestFilesReclaimer_APassWithMore(t *testing.T) {
	st, db, rec, _ := protocols(t, morePass(stale1, stale2)...)
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if !more || err != nil {
		t.Fatalf("Pass = (%v, %v), want (true, nil)", more, err)
	}
	log := out.String()
	if !strings.Contains(log, `level=INFO msg="sweep pass" files=0 directories=0 stale=1 more=true`) {
		t.Errorf("the pass's counts were not logged at info:\n%s", log)
	}
	sameOps(t, rec, moreOps...)
}

// The last pass of a backlog reports no more, and is logged with its
// counts.
func TestFilesReclaimer_TheLastPass(t *testing.T) {
	st, db, rec, _ := protocols(t, lastPass(stale1)...)
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if more || err != nil {
		t.Fatalf("Pass = (%v, %v), want (false, nil)", more, err)
	}
	if !strings.Contains(out.String(), `msg="sweep pass" files=0 directories=0 stale=1 more=false`) {
		t.Errorf("the pass's counts were not logged:\n%s", out)
	}
	sameOps(t, rec, moreOps...)
}

// A pass with nothing to do reports no more and logs nothing.
func TestFilesReclaimer_NothingToDo(t *testing.T) {
	st, db, rec, _ := protocols(t, idlePass()...)
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if more || err != nil || out.String() != "" {
		t.Errorf("Pass = (%v, %v), log %q; want (false, nil) and no record", more, err, out)
	}
	sameOps(t, rec, idleOps...)
}

// A pass's refusals are its error, returned beside its more for the
// sweeper to report and to go on by; the reclaimer logs the counts of the
// work the refused pass did, at info, and leaves the error to the
// sweeper's report, so a refusal is logged once, at warn.
func TestFilesReclaimer_RefusalsAreTheErrorAndItsCountsAreLogged(t *testing.T) {
	refused := sqltest.Response{Err: errors.New("the purge was refused")}
	st, db, rec, _ := protocols(t,
		// The first row refused, the next reclaimed, a third beyond the
		// budget.
		noRoots(), stale(stale1), refused, stale(stale2), exec(1), noRoots(), stale(stale3),
	)
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if !more || err == nil || !strings.Contains(err.Error(), "the purge was refused") {
		t.Fatalf("Pass = (%v, %v), want more and the refusal", more, err)
	}
	log := out.String()
	if !strings.Contains(log, `level=INFO msg="sweep pass" files=0 directories=0 stale=1 more=true`) {
		t.Errorf("the refused pass's counts were not logged at info:\n%s", log)
	}
	if strings.Contains(log, "the purge was refused") || strings.Contains(log, "level=WARN") {
		t.Errorf("the reclaimer logged the error the sweeper reports:\n%s", log)
	}
	sameOps(t, rec, q, q, x, q, x, q, q)
}

// A refused pass that did nothing reports its error and still logs its
// counts, all zero, at info, leaving the error to the sweeper's report.
func TestFilesReclaimer_ARefusalAloneLogsItsCounts(t *testing.T) {
	st, db, rec, _ := protocols(t, sqltest.Response{Err: errors.New("the read was refused")})
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if more || err == nil {
		t.Fatalf("Pass = (%v, %v), want (false, the refusal)", more, err)
	}
	log := out.String()
	if !strings.Contains(log, `level=INFO msg="sweep pass" files=0 directories=0 stale=0 more=false`) {
		t.Errorf("the refused pass's zero counts were not logged at info:\n%s", log)
	}
	if strings.Contains(log, "the read was refused") || strings.Contains(log, "level=WARN") {
		t.Errorf("the reclaimer logged the error the sweeper reports:\n%s", log)
	}
	sameOps(t, rec, q)
}

// A pass reads at most its budget of records, blobfs's batch, and reclaims
// only the rows older than the stale age: the pass's reads carry the
// budget as their limit and the stale read the cutoff an age ago.
func TestFilesReclaimer_TheBudgetAndTheStaleAge(t *testing.T) {
	st, db, rec, _ := protocols(t, idlePass()...)
	logger, _ := logged()
	before := time.Now()
	if _, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 7); err != nil {
		t.Fatalf("Pass = %v", err)
	}
	after := time.Now()
	calls := rec.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want the roots' read and the stale read", rec.Ops())
	}
	if args := calls[0].Args; len(args) == 0 || fmt.Sprint(args[len(args)-1]) != "7" {
		t.Errorf("the roots' read args = %v, want the budget, 7, as its limit", args)
	}
	args := calls[1].Args
	if len(args) == 0 || fmt.Sprint(args[len(args)-1]) != "7" {
		t.Errorf("the stale read args = %v, want the budget, 7, as its limit", args)
	}
	cutoff, ok := args[0].(time.Time)
	if !ok || cutoff.Before(before.Add(-staleAge)) || cutoff.After(after.Add(-staleAge)) {
		t.Errorf("the stale read's cutoff = %v, want the stale age, %v, before the pass", args[0], staleAge)
	}
}
