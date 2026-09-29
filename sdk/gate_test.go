package sdk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/standards-lab/go-web-service/sdk"
)

// acquire runs one acquisition in the background and reports its result,
// so a test can see whether it is still waiting.
func acquire(ctx context.Context, take func(context.Context) (func(), error)) <-chan func() {
	got := make(chan func(), 1)
	go func() {
		release, err := take(ctx)
		if err != nil {
			release = nil
		}
		got <- release
	}()
	return got
}

// waiting asserts that an acquisition has not completed yet.
func waiting(t *testing.T, got <-chan func(), what string) {
	t.Helper()
	settle()
	select {
	case <-got:
		t.Fatalf("%s acquired the gate, want it waiting", what)
	default:
	}
}

// hold takes the gate at once or fails the test.
func hold(t *testing.T, take func(context.Context) (func(), error)) func() {
	t.Helper()
	release, err := take(context.Background())
	if err != nil {
		t.Fatalf("acquire an open gate: %v", err)
	}
	return release
}

// Shared holders hold the gate together.
func TestGate_SharedHoldersCoexist(t *testing.T) {
	var g sdk.Gate
	a := hold(t, g.Shared)
	b := hold(t, g.Shared)
	a()
	b()
}

// An exclusive holder waits for every shared holder to release.
func TestGate_ExclusiveWaitsForShared(t *testing.T) {
	var g sdk.Gate
	a := hold(t, g.Shared)
	b := hold(t, g.Shared)
	ex := acquire(context.Background(), g.Exclusive)
	waiting(t, ex, "exclusive under two shared holders")
	a()
	waiting(t, ex, "exclusive under one shared holder")
	b()
	recvOrFail(t, ex, "exclusive once the shared holders released")()
}

// A shared holder waits out an exclusive holder, and so does another
// exclusive one.
func TestGate_SharedWaitsForExclusive(t *testing.T) {
	var g sdk.Gate
	release := hold(t, g.Exclusive)
	sh := acquire(context.Background(), g.Shared)
	waiting(t, sh, "shared under an exclusive holder")
	release()
	recvOrFail(t, sh, "shared once the exclusive holder released")()

	release = hold(t, g.Exclusive)
	ex := acquire(context.Background(), g.Exclusive)
	waiting(t, ex, "a second exclusive under the first")
	release()
	recvOrFail(t, ex, "the second exclusive once the first released")()
}

// A waiting exclusive holder bars new shared holders, so shared holders
// taking the gate in turns cannot starve it.
func TestGate_WaitingExclusiveBarsShared(t *testing.T) {
	var g sdk.Gate
	a := hold(t, g.Shared)
	ex := acquire(context.Background(), g.Exclusive)
	waiting(t, ex, "exclusive under a shared holder")
	sh := acquire(context.Background(), g.Shared)
	waiting(t, sh, "a new shared holder behind a waiting exclusive")
	a()
	release := recvOrFail(t, ex, "exclusive once the shared holder released")
	waiting(t, sh, "shared under the exclusive holder")
	release()
	recvOrFail(t, sh, "shared once the exclusive holder released")()
}

// An exclusive wait that gives up holds nothing and admits the shared
// holders it barred.
func TestGate_CancelledExclusiveAdmitsShared(t *testing.T) {
	var g sdk.Gate
	a := hold(t, g.Shared)
	defer a()
	ctx, cancel := context.WithCancel(context.Background())
	exErr := make(chan error, 1)
	go func() {
		_, err := g.Exclusive(ctx)
		exErr <- err
	}()
	settle()
	sh := acquire(context.Background(), g.Shared)
	waiting(t, sh, "shared behind a waiting exclusive")
	cancel()
	if err := recvOrFail(t, exErr, "the cancelled exclusive"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exclusive = %v, want the context's cancellation", err)
	}
	recvOrFail(t, sh, "shared once the exclusive withdrew")()
}

// A shared wait that gives up holds nothing: the exclusive holder's release
// leaves the gate open.
func TestGate_CancelledSharedHoldsNothing(t *testing.T) {
	var g sdk.Gate
	release := hold(t, g.Exclusive)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := g.Shared(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shared = %v, want the deadline", err)
	}
	release()
	hold(t, g.Exclusive)()
}

// An ended context acquires nothing, even from an open gate.
func TestGate_EndedContextAcquiresNothing(t *testing.T) {
	var g sdk.Gate
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Shared(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Shared = %v, want the cancellation", err)
	}
	if _, err := g.Exclusive(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Exclusive = %v, want the cancellation", err)
	}
	hold(t, g.Exclusive)()
}

// A release called twice releases once: a second shared holder still
// holds the gate.
func TestGate_ReleaseIsIdempotent(t *testing.T) {
	var g sdk.Gate
	a := hold(t, g.Shared)
	b := hold(t, g.Shared)
	a()
	a()
	ex := acquire(context.Background(), g.Exclusive)
	waiting(t, ex, "exclusive while a shared holder remains")
	b()
	recvOrFail(t, ex, "exclusive once the last shared holder released")()
}
