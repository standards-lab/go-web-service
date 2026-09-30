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
// proves the cold start and the startup contract: construction performs no
// I/O, and a failed start at stage 0 fails startup before the schema stage
// runs, instead of serving unready. The serve-probes-drain path and every
// behavior that needs a real engine are the integration tier's, the root
// integration package.

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

// New is the cold start and performs no I/O — it succeeds with nothing
// listening — and Run then fails startup at stage 0, draining to exit 1
// with the failure named in the log, never flipping readiness. The
// database and the object store start together, and the first to fail
// cancels the other, so the log names whichever failed first.
func TestRun_FailsStartupWithoutInfrastructure(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

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
		t.Errorf("failure log names neither stage-0 service: %q", out)
	}
	if strings.Contains(out, "server ready") {
		t.Error("log carries a ready record despite the failed startup")
	}
}

// A second Run cannot exist: the coordinator is single-use — spent by the
// first Run whether startup succeeded or not — and a re-run is a programming
// error that propagates go-core's panic.
func TestRun_TwicePanics(t *testing.T) {
	buf := &syncBuffer{}
	a, err := app.New(configtest.Config(t), buf)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = a.Run(ctx)

	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
	}()
	_ = a.Run(ctx)
}
