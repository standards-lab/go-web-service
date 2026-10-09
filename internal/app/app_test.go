package app_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// The suite is hermetic: no live database or object store exists, so it
// proves the cold start and the startup contract: the Build performs no
// I/O, and a failed start of the connections fails startup before the
// schema starts, instead of serving unready. The serve-probes-drain path
// and every behavior that needs a real engine are the integration tier's,
// the root integration package.

// failsafe bounds every wait for an event that should occur, so a broken
// composition fails the test instead of hanging it.
const failsafe = 2 * time.Second

// syncBuffer serializes writes so the app's logging goroutines and the
// test's reads stay race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// New and Run's Build perform no I/O — they succeed with nothing
// listening — and Run then fails startup at the connections' layer,
// draining to exit 1 with the failure named in the log, never flipping
// readiness. The database and the object store start together, and the
// first to fail cancels the other, so the log names whichever failed first.
func TestRun_FailsStartupWithoutInfrastructure(t *testing.T) {
	buf := &syncBuffer{}
	a := app.New(configtest.Config(t), buf)

	done := make(chan int, 1)
	go func() { done <- a.Run(context.Background()) }()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("Run = %d, want 1 on a failed startup", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to fail startup")
	}

	out := buf.String()
	if !strings.Contains(out, "startup: database") && !strings.Contains(out, "startup: storage") {
		t.Errorf("failure log names neither connection: %q", out)
	}
	if strings.Contains(out, "server ready") {
		t.Error("log carries a ready record despite the failed startup")
	}
}

// An App runs once. Run reports a Build or lifecycle failure as an exit
// code, but a second Run is a programming error and panics before it
// builds anything: the graph observes no node in it.
func TestRun_TwicePanics(t *testing.T) {
	a := app.New(configtest.Config(t), &syncBuffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx); code != 0 {
		t.Fatalf("first Run = %d, want 0 on a context ended before startup", code)
	}

	var built []string
	a.Graph().Observe(func(name string) { built = append(built, name) })
	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
		if len(built) != 0 {
			t.Errorf("the second Run built %v before it panicked, want nothing", built)
		}
	}()
	_ = a.Run(ctx)
}
