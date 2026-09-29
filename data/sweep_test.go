package data_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// waitFor bounds every wait for a pass that should run, so a broken
// worker fails the test instead of hanging it.
const waitFor = 2 * time.Second

// logBuffer serializes the worker's log writes and the test's reads.
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

// gate is the process's gate as the worker sees it, scripted: shared runs
// on each acquisition, before the pass, with the handler's context and the
// acquisition's number from 1, and released on its release.
type gate struct {
	shared   func(ctx context.Context, n int)
	released func(n int)

	mu sync.Mutex
	n  int
}

var _ data.SweepGate = (*gate)(nil)

func (g *gate) Shared(ctx context.Context) (func(), error) {
	g.mu.Lock()
	g.n++
	n := g.n
	g.mu.Unlock()
	if g.shared != nil {
		g.shared(ctx, n)
	}
	return func() {
		if g.released != nil {
			g.released(n)
		}
	}, nil
}

func (g *gate) acquisitions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}

// The sweep's options in the scripts: a batch of one record, so each pass
// is a few statements, and the stale reclaim, whose rows the scripts use.
var sweepOpts = []bfdata.SweepOption{bfdata.Batch(1), bfdata.StaleOlderThan(time.Hour)}

// noRoots scripts a read of the deleting branches' roots that finds none.
func noRoots() sqltest.Response {
	return sqltest.Response{Columns: []string{"id", "parent_id", "name", "status", "version", "created_at", "updated_at"}}
}

// stale is a deleting file row past the stale age, one per id.
func stale(id string) sqltest.Response {
	f := file(blobfs.StatusDeleting, 3)
	f.ID, f.Key = id, id+"/report.txt"
	f.UpdatedAt = f.UpdatedAt.Add(-2 * time.Hour)
	return fileRows(f)
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
	return []sqltest.Response{noRoots(), stale(id), exec(1), noRoots(), fileRows()}
}

// idlePass scripts a pass with nothing to do.
func idlePass() []sqltest.Response { return []sqltest.Response{noRoots(), fileRows()} }

var (
	moreOps = []sqltest.Op{q, q, x, q, q}
	idleOps = []sqltest.Op{q, q}
)

func script(passes ...[]sqltest.Response) []sqltest.Response {
	var out []sqltest.Response
	for _, p := range passes {
		out = append(out, p...)
	}
	return out
}

func ops(passes ...[]sqltest.Op) []sqltest.Op {
	var out []sqltest.Op
	for _, p := range passes {
		out = append(out, p...)
	}
	return out
}

// A wake runs passes while one reports More and stops at the first that
// does not, logging each pass that did work.
func TestSweepWorker_LoopsWhileMore(t *testing.T) {
	st, db, rec, _ := protocols(t, script(morePass(stale1, stale2), lastPass(stale2))...)
	logger, out := logged()
	g := &gate{}
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(context.Background(), time.Now()); err != nil {
		t.Fatalf("worker = %v, want nil", err)
	}
	if g.acquisitions() != 2 {
		t.Errorf("passes = %d, want 2: one with More, then the last", g.acquisitions())
	}
	if n := strings.Count(out.String(), "msg=\"sweep pass\""); n != 2 {
		t.Errorf("logged %d passes, want each pass that did work:\n%s", n, out)
	}
	sameOps(t, rec, ops(moreOps, moreOps)...)
}

// A pass with nothing to do is one pass and no record.
func TestSweepWorker_NothingToDo(t *testing.T) {
	st, db, rec, _ := protocols(t, idlePass()...)
	logger, out := logged()
	g := &gate{}
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(context.Background(), time.Now()); err != nil {
		t.Fatalf("worker = %v, want nil", err)
	}
	if g.acquisitions() != 1 || out.String() != "" {
		t.Errorf("passes = %d, log %q; want one pass and no record", g.acquisitions(), out)
	}
	sameOps(t, rec, idleOps...)
}

// A pass's refusals are logged at warn with the error and the counts and
// never returned: the loop goes on while the pass reports More, and ends
// with nil when it does not, so a stuck row cannot end the reactor that
// runs the worker.
func TestSweepWorker_RefusalsAreLoggedNotFailed(t *testing.T) {
	refused := sqltest.Response{Err: errors.New("the purge was refused")}
	st, db, rec, _ := protocols(t,
		// A pass with More: the first row refused, the next reclaimed, a
		// third beyond the batch.
		noRoots(), stale(stale1), refused, stale(stale2), exec(1), noRoots(), stale(stale3),
		// A pass without: the third row refused, nothing beyond it.
		noRoots(), stale(stale3), refused, fileRows(),
	)
	logger, out := logged()
	g := &gate{}
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(context.Background(), time.Now()); err != nil {
		t.Fatalf("worker = %v, want nil despite the refusals", err)
	}
	if g.acquisitions() != 2 {
		t.Errorf("passes = %d, want 2: a refusal does not stop a pass with More", g.acquisitions())
	}
	log := out.String()
	if n := strings.Count(log, "level=WARN msg=\"sweep pass refused\""); n != 2 {
		t.Errorf("logged %d refusals at warn, want 2:\n%s", n, log)
	}
	if !strings.Contains(log, "the purge was refused") || !strings.Contains(log, "stale=1") {
		t.Errorf("a refusal's record lacks the error or the counts:\n%s", log)
	}
	sameOps(t, rec, q, q, x, q, x, q, q, q, q, x, q)
}

// The context is checked before each pass: once it ends, no further pass
// runs and the worker returns its error, the drain's cancellation.
func TestSweepWorker_ContextStopsTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, db, rec, _ := protocols(t, script(morePass(stale1, stale2))...)
	logger, _ := logged()
	// The second acquisition ends the context, so the second pass reaches
	// no statement.
	g := &gate{shared: func(_ context.Context, n int) {
		if n == 2 {
			cancel()
		}
	}}
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("worker = %v, want the context's cancellation", err)
	}
	if g.acquisitions() != 2 {
		t.Errorf("passes begun = %d, want 2: none after the context ended", g.acquisitions())
	}
	sameOps(t, rec, moreOps...)

	st, db, rec, _ = protocols(t)
	g = &gate{}
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(ctx, time.Now()); !errors.Is(err, context.Canceled) || g.acquisitions() != 0 {
		t.Errorf("worker on an ended context = %v after %d passes, want no pass", err, g.acquisitions())
	}
	sameOps(t, rec)
}

// Under the reactor the composition root stages it on, a refused pass
// leaves the reactor running: Err yields nothing, and the next nudge runs
// the next pass.
func TestSweepWorker_ARefusalKeepsTheReactor(t *testing.T) {
	st, db, rec, _ := protocols(t,
		sqltest.Response{Err: errors.New("the read was refused")}, // the first pass's roots
		noRoots(), // the second pass's
	)
	ran := make(chan struct{}, 2)
	g := &gate{released: func(int) { ran <- struct{}{} }}
	logger, _ := logged()
	wake := sdk.Wake(time.Hour)
	r := sdk.New(wake, st.SweepWorker(db, g, logger))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		wake.Nudge()
		select {
		case <-ran:
		case <-time.After(waitFor):
			t.Fatalf("pass %d did not run on its nudge", i+1)
		}
	}
	select {
	case err := <-r.Err():
		t.Errorf("Err yielded %v after a refusal, want the reactor running", err)
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown = %v", err)
	}
	sameOps(t, rec, q, q)
}

// Once the drain begins, the pass in flight finishes and no further pass
// runs, though it reported More: the worker returns nil, and the drain
// ends clean rather than when a grace cancels it.
func TestSweepWorker_TheDrainStopsBetweenPasses(t *testing.T) {
	st, db, rec, _ := protocols(t, script(morePass(stale1, stale2))...)
	wake := sdk.Wake(time.Hour)
	var r *sdk.Reactor[time.Time]
	drained := make(chan error, 1)
	// The first pass begins the drain and waits for its signal before it
	// runs, so the drain begins while the pass is in flight.
	g := &gate{shared: func(ctx context.Context, n int) {
		if n != 1 {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), waitFor)
			defer cancel()
			drained <- r.Shutdown(ctx)
		}()
		select {
		case <-sdk.Draining(ctx):
		case <-time.After(waitFor):
			t.Error("the drain signal never reached the pass")
		}
	}}
	logger, out := logged()
	r = sdk.New(wake, st.SweepWorker(db, g, logger, sweepOpts...))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	wake.Nudge()
	select {
	case err := <-drained:
		if err != nil {
			t.Errorf("Shutdown = %v, want a clean drain", err)
		}
	case <-time.After(waitFor):
		t.Fatal("the drain did not finish")
	}
	if g.acquisitions() != 1 {
		t.Errorf("passes = %d, want the one in flight alone", g.acquisitions())
	}
	if !strings.Contains(out.String(), "more=true") {
		t.Errorf("the pass in flight was not finished and logged:\n%s", out)
	}
	sameOps(t, rec, moreOps...)
}

// A pass waits out a schema change: while the gate is held exclusively no
// pass runs, and once it is released the wake's passes run.
func TestSweepWorker_APassWaitsOutASchemaChange(t *testing.T) {
	st, db, rec, _ := protocols(t, idlePass()...)
	g := new(sdk.Gate)
	change, err := g.Exclusive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	logger, _ := logged()
	done := make(chan error, 1)
	go func() { done <- st.SweepWorker(db, g, logger, sweepOpts...)(context.Background(), time.Now()) }()

	time.Sleep(50 * time.Millisecond)
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("a pass ran under the schema change: %v", rec.Ops())
	}
	change()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("worker = %v, want nil", err)
		}
	case <-time.After(waitFor):
		t.Fatal("no pass ran once the schema change released the gate")
	}
	sameOps(t, rec, idleOps...)
}

// Each pass runs whole inside its hold of the gate: none of its statements
// runs before the acquisition or after the release, so a schema change,
// which holds the gate exclusively, waits for the pass in flight.
func TestSweepWorker_APassRunsInsideItsHold(t *testing.T) {
	st, db, rec, _ := protocols(t, script(morePass(stale1, stale2), lastPass(stale2))...)
	var mu sync.Mutex
	var at []int
	record := func() {
		mu.Lock()
		defer mu.Unlock()
		at = append(at, len(rec.Calls()))
	}
	g := &gate{shared: func(context.Context, int) { record() }, released: func(int) { record() }}
	logger, _ := logged()
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(context.Background(), time.Now()); err != nil {
		t.Fatalf("worker = %v", err)
	}
	// The statements recorded at each acquisition and release: each pass's
	// five between its own two.
	if want := []int{0, 5, 5, 10}; len(at) != len(want) || at[0] != want[0] || at[1] != want[1] || at[2] != want[2] || at[3] != want[3] {
		t.Errorf("statements at each acquisition and release = %v, want %v", at, want)
	}
	sameOps(t, rec, ops(moreOps, moreOps)...)
}

// A drain that ends the wake while a pass waits on the gate runs no pass,
// and the worker returns the cancellation.
func TestSweepWorker_TheDrainEndsAWaitingPass(t *testing.T) {
	st, db, rec, _ := protocols(t)
	g := new(sdk.Gate)
	change, err := g.Exclusive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer change()
	logger, out := logged()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := st.SweepWorker(db, g, logger, sweepOpts...)(ctx, time.Now()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker = %v, want the context's end", err)
	}
	if out.String() != "" {
		t.Errorf("log %q; want no record", out)
	}
	sameOps(t, rec)
}
