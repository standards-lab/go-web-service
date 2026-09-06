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

// The suite is hermetic: no live database exists, so it proves the cold
// start and the startup contract: construction performs no I/O, and a
// failed database ping at stage 0 fails startup before the schema stage
// runs, instead of serving unready. The serve-probes-drain path is proven
// against the live compose database; the integration tier
// (v1.data.sql.tasks.suite) is its home.

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

// New is the cold start and performs no I/O — it succeeds with no database
// listening — and Run then fails startup on the dead database's ping,
// draining to exit 1 with the failure named in the log, never flipping
// readiness.
func TestRun_FailsStartupWithoutDatabase(t *testing.T) {
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
			t.Errorf("Run = %d, want 1 on a failed database startup", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to fail startup")
	}

	out := buf.String()
	if !strings.Contains(out, "database") {
		t.Errorf("failure log does not name the database: %q", out)
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
