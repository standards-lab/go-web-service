package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	bfdata "github.com/standards-lab/blobfs/data"

	"github.com/standards-lab/go-web-service/sdk"
)

// waitFor bounds every wait for a pass that should run, so a broken
// reactor fails the test instead of hanging it.
const waitFor = 2 * time.Second

// logBuffer serializes the reactor's log writes and the test's reads.
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

// passes scripts the sweep's passes: each call returns the next result,
// and a call past the script fails the test.
type passes struct {
	t       *testing.T
	results []bfdata.SweepResult
	errs    []error
	calls   int
	// during runs inside the call with the given index, before it returns.
	during map[int]func()
}

func (p *passes) pass(context.Context) (bfdata.SweepResult, error) {
	i := p.calls
	p.calls++
	if i >= len(p.results) {
		p.t.Errorf("pass %d past the script of %d", i+1, len(p.results))
		return bfdata.SweepResult{}, nil
	}
	if fn := p.during[i]; fn != nil {
		fn()
	}
	var err error
	if i < len(p.errs) {
		err = p.errs[i]
	}
	return p.results[i], err
}

// logged is a logger over a buffer the test reads.
func logged() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

// A wake runs passes while one reports More and stops at the first that
// does not.
func TestSweep_LoopsWhileMore(t *testing.T) {
	p := &passes{t: t, results: []bfdata.SweepResult{
		{Files: 2, More: true},
		{Directories: 1, More: true},
		{Stale: 1},
	}}
	logger, out := logged()
	if err := sweep(p.pass, logger)(context.Background(), time.Now()); err != nil {
		t.Fatalf("sweep = %v, want nil", err)
	}
	if p.calls != 3 {
		t.Errorf("passes = %d, want 3: two with More, then the last", p.calls)
	}
	if n := strings.Count(out.String(), "msg=\"sweep pass\""); n != 3 {
		t.Errorf("logged %d passes, want each pass that did work:\n%s", n, out)
	}
}

// A pass with nothing to do is one pass and no record.
func TestSweep_NothingToDo(t *testing.T) {
	p := &passes{t: t, results: []bfdata.SweepResult{{}}}
	logger, out := logged()
	if err := sweep(p.pass, logger)(context.Background(), time.Now()); err != nil {
		t.Fatalf("sweep = %v, want nil", err)
	}
	if p.calls != 1 || out.String() != "" {
		t.Errorf("passes = %d, log %q; want one pass and no record", p.calls, out)
	}
}

// A pass's refusals are logged at warn with the error and the counts and
// never returned: the loop goes on while the pass reports More, and ends
// with nil when it does not, so a stuck row cannot end the reactor.
func TestSweep_RefusalsAreLoggedNotFailed(t *testing.T) {
	refused := errors.New("data: sweep: branch b1: the object store refused")
	p := &passes{
		t:       t,
		results: []bfdata.SweepResult{{Files: 1, More: true}, {Directories: 1}},
		errs:    []error{refused, refused},
	}
	logger, out := logged()
	if err := sweep(p.pass, logger)(context.Background(), time.Now()); err != nil {
		t.Fatalf("sweep = %v, want nil despite the refusals", err)
	}
	if p.calls != 2 {
		t.Errorf("passes = %d, want 2: a refusal does not stop a pass with More", p.calls)
	}
	log := out.String()
	if n := strings.Count(log, "level=WARN msg=\"sweep pass refused\""); n != 2 {
		t.Errorf("logged %d refusals at warn, want 2:\n%s", n, log)
	}
	if !strings.Contains(log, "the object store refused") || !strings.Contains(log, "files=1") {
		t.Errorf("a refusal's record lacks the error or the counts:\n%s", log)
	}
}

// The context is checked before each pass: once it ends, no further pass
// runs and the Func returns its error, the drain's cancellation.
func TestSweep_ContextStopsTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &passes{
		t:       t,
		results: []bfdata.SweepResult{{Files: 1, More: true}, {Files: 1, More: true}, {Files: 1, More: true}},
		during:  map[int]func(){1: cancel},
	}
	logger, _ := logged()
	if err := sweep(p.pass, logger)(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("sweep = %v, want the context's cancellation", err)
	}
	if p.calls != 2 {
		t.Errorf("passes = %d, want 2: none after the context ended", p.calls)
	}

	p = &passes{t: t}
	if err := sweep(p.pass, logger)(ctx, time.Now()); !errors.Is(err, context.Canceled) || p.calls != 0 {
		t.Errorf("sweep on an ended context = %v after %d passes, want no pass", err, p.calls)
	}
}

// Under the reactor, a refused pass leaves the reactor running: Err
// yields nothing, and the next nudge runs the next pass.
func TestSweep_ARefusalKeepsTheReactor(t *testing.T) {
	ran := make(chan struct{}, 2)
	p := &passes{
		t:       t,
		results: []bfdata.SweepResult{{}, {Directories: 1}},
		errs:    []error{errors.New("refused")},
		during:  map[int]func(){0: func() { ran <- struct{}{} }, 1: func() { ran <- struct{}{} }},
	}
	logger, _ := logged()
	wake := sdk.Wake(time.Hour)
	r := sdk.New(wake, sweep(p.pass, logger))
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
}
