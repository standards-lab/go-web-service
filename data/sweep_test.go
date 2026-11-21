package data_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

var _ sweep.Reclaimer = (*data.FilesReclaimer)(nil)

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

func logged() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

const staleAge = time.Hour

func noRoots() sqltest.Response { return datatest.DirectoryRows() }

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

func morePass(id, next string) []sqltest.Response {
	return []sqltest.Response{noRoots(), stale(id), exec(1), noRoots(), stale(next)}
}

func lastPass(id string) []sqltest.Response {
	return []sqltest.Response{noRoots(), stale(id), exec(1), noRoots(), datatest.FileRows()}
}

func idlePass() []sqltest.Response { return []sqltest.Response{noRoots(), datatest.FileRows()} }

var (
	moreOps = []sqltest.Op{q, q, x, q, q}
	idleOps = []sqltest.Op{q, q}
)

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

func TestFilesReclaimer_NothingToDo(t *testing.T) {
	st, db, rec, _ := protocols(t, idlePass()...)
	logger, out := logged()
	more, err := st.FilesReclaimer(db, staleAge, logger).Pass(context.Background(), 1)
	if more || err != nil || out.String() != "" {
		t.Errorf("Pass = (%v, %v), log %q; want (false, nil) and no record", more, err, out)
	}
	sameOps(t, rec, idleOps...)
}

func TestFilesReclaimer_RefusalsAreTheErrorAndItsCountsAreLogged(t *testing.T) {
	refused := sqltest.Response{Err: errors.New("the purge was refused")}
	firstRefused := []sqltest.Response{noRoots(), stale(stale1), refused}
	nextReclaimed := []sqltest.Response{stale(stale2), exec(1)}
	thirdBeyondTheBudget := []sqltest.Response{noRoots(), stale(stale3)}
	st, db, rec, _ := protocols(t, slices.Concat(firstRefused, nextReclaimed, thirdBeyondTheBudget)...)
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
