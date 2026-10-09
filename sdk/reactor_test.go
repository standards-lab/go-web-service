package sdk_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/sdk"
)

// failsafe bounds every wait for something that should happen, so a broken
// reactor fails the test instead of hanging it.
const failsafe = 2 * time.Second

func recvOrFail[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(failsafe):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(failsafe)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func shutdown(t *testing.T, r interface{ Shutdown(context.Context) error }, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return r.Shutdown(ctx)
}

func TestReactor_ShutdownDrainsInFlight(t *testing.T) {
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	var ended, finished atomic.Bool
	r := sdk.New(sdk.Every(5*time.Millisecond), func(ctx context.Context, _ time.Time) error {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(50 * time.Millisecond)
		ended.Store(ctx.Err() != nil)
		finished.Store(true)
		return nil
	})

	runCtx, cancelRun := context.WithCancel(context.Background())
	if err := r.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the first tick")
	cancelRun() // the coordinator cancels the run context before draining

	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !finished.Load() {
		t.Error("Shutdown returned before the in-flight handler finished")
	}
	if ended.Load() {
		t.Error("the handler's context ended before the drain finished")
	}
	n := calls.Load()
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != n {
		t.Error("a tick was handled after Shutdown returned")
	}
}

func TestReactor_RunContextDoesNotStop(t *testing.T) {
	var calls atomic.Int32
	r := sdk.New(sdk.Every(2*time.Millisecond), func(context.Context, time.Time) error {
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	eventually(t, func() bool { return calls.Load() >= 3 }, "ticks after the run context ended")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

func TestReactor_ShutdownDeadlineCancelsHandler(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan error, 1)
	r := sdk.New(sdk.Every(time.Millisecond), func(ctx context.Context, _ time.Time) error {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- ctx.Err()
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the handler")
	err := shutdown(t, r, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want DeadlineExceeded", err)
	}
	if got := recvOrFail(t, cancelled, "the handler's cancellation"); !errors.Is(got, context.Canceled) {
		t.Errorf("handler context error = %v", got)
	}
}

func TestReactor_GraceCancelsThenWaits(t *testing.T) {
	started := make(chan struct{}, 1)
	unwound := make(chan struct{})
	r := sdk.New(sdk.Every(time.Millisecond), func(ctx context.Context, _ time.Time) error {
		started <- struct{}{}
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond) // the handler takes time to unwind
		close(unwound)
		return ctx.Err()
	}, sdk.Grace(20*time.Millisecond))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the handler")
	err := shutdown(t, r, failsafe)
	select {
	case <-unwound:
	default:
		t.Fatal("Shutdown returned before the handler unwound")
	}
	if err == nil || !strings.Contains(err.Error(), "handlers cancelled after grace 20ms") {
		t.Fatalf("Shutdown = %v, want the grace report", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("Shutdown = %v, reports the handler's own cancellation", err)
	}
}

func TestReactor_GraceStillBoundedByContext(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	r := sdk.New(sdk.Every(time.Millisecond), func(context.Context, time.Time) error {
		started <- struct{}{}
		<-release // ignores cancellation
		return nil
	}, sdk.Grace(10*time.Millisecond))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, started, "the handler")
	if err := shutdown(t, r, 50*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want DeadlineExceeded", err)
	}
}

func TestReactor_GraceUnusedIsClean(t *testing.T) {
	r := sdk.New(sdk.Every(time.Millisecond), func(context.Context, time.Time) error {
		return nil
	}, sdk.Grace(time.Second))
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
}

// A handler's error ends an interval source, and so the reactor, which
// reports it on Err, closes Err, and reads not ready; the failure is not
// reported a second time by Shutdown.
func TestReactor_HandlerErrorOnErr(t *testing.T) {
	boom := errors.New("boom")
	r := sdk.New(sdk.Every(time.Millisecond), func(context.Context, time.Time) error {
		return boom
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recvOrFail(t, r.Err(), "the failure"); !errors.Is(err, boom) {
		t.Fatalf("Err yielded %v", err)
	}
	if _, open := <-r.Err(); open {
		t.Error("Err did not close after the source ended")
	}
	if r.Ready() {
		t.Error("a failed reactor reports ready")
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown after a reported failure = %v, want nil", err)
	}
}

func TestReactor_UnreadErrReachesShutdown(t *testing.T) {
	boom := errors.New("boom")
	src := failing{err: boom, failed: make(chan struct{})}
	r := sdk.New[int](src, func(context.Context, int) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Nothing reads Err, as when the coordinator stopped monitoring at the
	// signal just before the source failed. Let the failure land on Err
	// while the reactor is still running.
	recvOrFail(t, src.failed, "the source's failure")
	time.Sleep(20 * time.Millisecond)
	if err := shutdown(t, r, failsafe); !errors.Is(err, boom) {
		t.Fatalf("Shutdown = %v, want the unread failure", err)
	}
}

func TestReactor_ErrorWhileDrainingGoesToShutdown(t *testing.T) {
	boom := errors.New("boom")
	src := &stub{err: boom}
	r := sdk.New[int](src, func(context.Context, int) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); !errors.Is(err, boom) {
		t.Fatalf("Shutdown = %v, want boom", err)
	}
	if _, open := <-r.Err(); open {
		t.Error("Err yielded an error sent to Shutdown")
	}
}

func TestReactor_Ready(t *testing.T) {
	r := sdk.New(sdk.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if r.Ready() {
		t.Error("ready before Start")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
	if r.Ready() {
		t.Error("ready after Shutdown")
	}
}

func TestReactor_StartTwice(t *testing.T) {
	r := sdk.New(sdk.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err == nil {
		t.Error("second Start succeeded")
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

func TestReactor_ShutdownBeforeStart(t *testing.T) {
	r := sdk.New(sdk.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown before Start = %v", err)
	}
	if err := r.Start(context.Background()); err == nil {
		t.Error("Start after Shutdown succeeded")
	}
	if _, open := <-r.Err(); open {
		t.Error("Err is open on a retired reactor")
	}
}

// A second Shutdown of a reactor retired before Start returns nil: it has
// no source to stop.
func TestReactor_ShutdownTwiceBeforeStart(t *testing.T) {
	r := sdk.New(sdk.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	for i := range 2 {
		if err := shutdown(t, r, failsafe); err != nil {
			t.Errorf("Shutdown %d before Start = %v", i+1, err)
		}
	}
}

// A second Shutdown of a drained reactor returns at once, with the first's
// outcome.
func TestReactor_ShutdownTwiceAfterStart(t *testing.T) {
	r := sdk.New(sdk.Every(time.Hour), func(context.Context, time.Time) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := shutdown(t, r, failsafe); err != nil {
			t.Errorf("Shutdown %d = %v", i+1, err)
		}
	}
}

// The drain signal closes when Shutdown begins, while the handler in
// flight still runs on a live context, so a handler that works in steps
// stops between them and the drain ends clean, without a grace.
func TestReactor_DrainingClosesWhenShutdownBegins(t *testing.T) {
	wake := sdk.Wake(time.Hour)
	inFlight := make(chan struct{})
	type seen struct {
		drained bool
		ctxErr  error
		steps   int
	}
	got := make(chan seen, 1)
	r := sdk.New(wake, func(ctx context.Context, _ time.Time) error {
		close(inFlight)
		steps := 0
		for {
			steps++
			select {
			case <-sdk.Draining(ctx):
				got <- seen{true, ctx.Err(), steps}
				return nil
			case <-time.After(time.Millisecond):
			}
		}
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	wake.Nudge()
	recvOrFail(t, inFlight, "the handler to start")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatalf("Shutdown = %v, want a clean drain", err)
	}
	res := recvOrFail(t, got, "the handler to see the drain")
	if !res.drained || res.ctxErr != nil || res.steps == 0 {
		t.Errorf("handler saw %+v; want the drain signal on a live context", res)
	}
}

// A context no reactor gave has no drain signal: Draining is nil, which
// never closes.
func TestDraining_OutsideAReactor(t *testing.T) {
	if ch := sdk.Draining(context.Background()); ch != nil {
		t.Errorf("Draining(Background) = %v, want nil", ch)
	}
}

func TestReactor_HandlerKeepsSourceDeadline(t *testing.T) {
	src := &deadlineSource{d: 50 * time.Millisecond, stopped: make(chan struct{})}
	type result struct {
		hasDeadline bool
		err         error
	}
	got := make(chan result, 1)
	r := sdk.New[int](src, func(ctx context.Context, _ int) error {
		<-src.stopped
		_, ok := ctx.Deadline()
		<-ctx.Done()
		got <- result{ok, ctx.Err()}
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := recvOrFail(t, got, "the handler's context to end")
	if !res.hasDeadline {
		t.Error("the handler lost the source's deadline")
	}
	if !errors.Is(res.err, context.DeadlineExceeded) {
		t.Errorf("handler context ended with %v, want the deadline, not the source's stop", res.err)
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// A reactor joins a graph-backed coordinator as a node's value with no
// adapter: the coordinator infers from its methods that it starts, drains,
// reports readiness, and is monitored.
var _ interface {
	lifecycle.Subsystem
	lifecycle.ReadinessChecker
	lifecycle.Monitored
} = (*sdk.Reactor[time.Time])(nil)

// coordinate builds a one-node graph whose value is r and returns a
// coordinator over it, which shuts down within failsafe.
func coordinate(t *testing.T, r *sdk.Reactor[time.Time]) *lifecycle.Coordinator {
	t.Helper()
	g := graph.New()
	node := g.Define("reactor", func(*graph.Scope) (*sdk.Reactor[time.Time], error) {
		return r, nil
	})
	sys, err := g.Build(node)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return lifecycle.New(sys, lifecycle.Config{ShutdownTimeout: libconfig.Duration(failsafe)})
}

// As a graph node's value, registered nowhere, a handler's failure while
// running ends the run with the failure in Run's result, and the drain
// still shuts the reactor down.
func TestReactor_CoordinatorAdapter(t *testing.T) {
	boom := errors.New("boom")
	r := sdk.New(sdk.Every(time.Millisecond), func(context.Context, time.Time) error {
		return boom
	}, sdk.Grace(10*time.Millisecond))
	lc := coordinate(t, r)

	done := make(chan error, 1)
	go func() { done <- lc.Run(context.Background()) }()
	if err := recvOrFail(t, done, "Run to end on the reactor's failure"); !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want the handler's failure", err)
	}
}

// A clean signal drains a reactor that is a graph node's value to a nil
// Run result.
func TestReactor_CoordinatorDrainsClean(t *testing.T) {
	r := sdk.New(sdk.Every(time.Millisecond), func(context.Context, time.Time) error {
		return nil
	}, sdk.Grace(10*time.Millisecond))
	lc := coordinate(t, r)

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	lc.OnReady(func() { close(ready) })
	done := make(chan error, 1)
	go func() { done <- lc.Run(ctx) }()
	recvOrFail(t, ready, "the coordinator's readiness")
	cancel()
	if err := recvOrFail(t, done, "Run to drain"); err != nil {
		t.Fatalf("Run = %v, want a clean drain", err)
	}
}

func TestEvery_DropsStaleTicks(t *testing.T) {
	const interval = 20 * time.Millisecond
	lags := make(chan time.Duration, 3)
	r := sdk.New(sdk.Every(interval), func(_ context.Context, at time.Time) error {
		select {
		case lags <- time.Since(at):
		default:
		}
		time.Sleep(3 * interval) // outlast several ticks
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if lag := recvOrFail(t, lags, "a tick"); lag > interval {
			t.Errorf("tick delivered %v late; a stale tick was kept", lag)
		}
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

func TestEvery_RejectsNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Every(0) did not panic")
		}
	}()
	sdk.Every(0)
}

// counter is a Func that counts its runs and reports each on runs.
type counter struct {
	calls atomic.Int32
	runs  chan struct{}
}

func newCounter() *counter { return &counter{runs: make(chan struct{}, 64)} }

func (c *counter) fn(context.Context, time.Time) error {
	c.calls.Add(1)
	c.runs <- struct{}{}
	return nil
}

// settle waits long enough for a delivery that should not happen to show.
func settle() { time.Sleep(50 * time.Millisecond) }

// Wake without a nudge is an interval: it delivers on every tick.
func TestWake_Interval(t *testing.T) {
	c := newCounter()
	r := sdk.New(sdk.Wake(5*time.Millisecond), c.fn)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return c.calls.Load() >= 3 }, "three ticks")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

// Nudges before the next delivery coalesce into one run; a nudge after it
// is a run of its own.
func TestWake_NudgesCoalesce(t *testing.T) {
	c := newCounter()
	w := sdk.Wake(time.Hour)
	for range 5 {
		w.Nudge()
	}
	r := sdk.New(w, c.fn)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recvOrFail(t, c.runs, "the nudged run")
	settle()
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("five nudges before the run ran %d times, want 1", n)
	}

	w.Nudge()
	recvOrFail(t, c.runs, "the second nudged run")
	settle()
	if n := c.calls.Load(); n != 2 {
		t.Errorf("a nudge after the run brought the total to %d, want 2", n)
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

// A nudge while the handler runs, however many, produces exactly one run
// after it: work that arrived during the run is not lost, and is not run
// twice.
func TestWake_NudgeDuringRun(t *testing.T) {
	var calls atomic.Int32
	running := make(chan struct{}, 1)
	release := make(chan struct{})
	w := sdk.Wake(time.Hour)
	r := sdk.New(w, func(context.Context, time.Time) error {
		if calls.Add(1) == 1 {
			running <- struct{}{}
			<-release
		}
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Nudge()
	recvOrFail(t, running, "the first run")
	for range 3 {
		w.Nudge()
	}
	close(release)
	eventually(t, func() bool { return calls.Load() == 2 }, "the run after the nudges")
	settle()
	if n := calls.Load(); n != 2 {
		t.Errorf("nudges during a run brought the total to %d, want 2", n)
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
}

// A nudge after Shutdown runs nothing and does not block.
func TestWake_NoRunAfterShutdown(t *testing.T) {
	c := newCounter()
	w := sdk.Wake(time.Hour)
	r := sdk.New(w, c.fn)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, r.Ready, "readiness")
	if err := shutdown(t, r, failsafe); err != nil {
		t.Fatal(err)
	}
	w.Nudge()
	w.Nudge()
	settle()
	if n := c.calls.Load(); n != 0 {
		t.Errorf("nudges after Shutdown ran %d times, want 0", n)
	}
	if w.Ready() || r.Ready() {
		t.Error("ready after Shutdown")
	}
}

// A handler's error ends Wake as it ends Every, reported on Err.
func TestWake_HandlerErrorOnErr(t *testing.T) {
	boom := errors.New("boom")
	w := sdk.Wake(time.Hour)
	r := sdk.New(w, func(context.Context, time.Time) error { return boom })
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Nudge()
	if err := recvOrFail(t, r.Err(), "the failure"); !errors.Is(err, boom) {
		t.Fatalf("Err yielded %v", err)
	}
	if err := shutdown(t, r, failsafe); err != nil {
		t.Errorf("Shutdown after a reported failure = %v, want nil", err)
	}
}

func TestWake_RejectsNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Wake(0) did not panic")
		}
	}()
	sdk.Wake(0)
}

// stub is a source that is ready until its context ends, then returns err.
type stub struct {
	err   error
	ready atomic.Bool
}

func (s *stub) Receive(ctx context.Context, _ sdk.Func[int]) error {
	s.ready.Store(true)
	<-ctx.Done()
	s.ready.Store(false)
	return s.err
}

func (s *stub) Ready() bool { return s.ready.Load() }

// failing is a source that fails as soon as it starts, closing failed as
// it returns.
type failing struct {
	err    error
	failed chan struct{}
}

func (f failing) Receive(context.Context, sdk.Func[int]) error {
	defer close(f.failed)
	return f.err
}

func (failing) Ready() bool { return false }

// deadlineSource delivers one occurrence under a per-occurrence deadline,
// then stops its own context while the handler still runs.
type deadlineSource struct {
	d       time.Duration
	stopped chan struct{}
}

func (s *deadlineSource) Receive(ctx context.Context, fn sdk.Func[int]) error {
	occ, cancel := context.WithTimeout(ctx, s.d)
	defer cancel()
	stop, stopNow := context.WithCancel(occ)
	done := make(chan error, 1)
	go func() { done <- fn(stop, 1) }()
	stopNow()
	close(s.stopped)
	<-done
	<-ctx.Done()
	return nil
}

func (s *deadlineSource) Ready() bool { return true }
