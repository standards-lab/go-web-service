package sdk

import (
	"context"
	"sync"
)

// The gate tenant, bound for go-core beside the reactor: a quiesce gate a
// process's background work holds shared and an operation that must run
// with that work paused holds exclusively. It is a readers-writer lock
// whose acquisitions honor a context, so a caller that gives up stops
// waiting, and which prefers the exclusive side: once an exclusive holder
// is waiting, no new shared holder is admitted, so work that holds the gate
// in short, repeated turns cannot starve the operation waiting for it.

// Gate is a quiesce gate. Any number of shared holders may hold it at once,
// or one exclusive holder and no shared one. The zero value is an open
// gate, ready to use; a Gate must not be copied after first use.
type Gate struct {
	mu       sync.Mutex
	shared   int  // shared holders
	held     bool // an exclusive holder
	queued   int  // exclusive waiters, which bar new shared holders
	released chan struct{}
}

// Shared holds the gate alongside any other shared holder, waiting while
// an exclusive holder holds it or waits for it. It returns the release,
// which is safe to call more than once, or ctx's error if ctx ends first,
// holding nothing.
func (g *Gate) Shared(ctx context.Context) (release func(), err error) {
	g.mu.Lock()
	for g.held || g.queued > 0 {
		if err := g.wait(ctx); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	g.shared++
	g.mu.Unlock()
	return g.release(func() { g.shared-- }), nil
}

// Exclusive holds the gate alone, waiting until every shared holder and
// any exclusive one has released it; from the moment it waits, no new
// shared holder is admitted. It returns the release, which is safe to call
// more than once, or ctx's error if ctx ends first, holding nothing and
// admitting the shared holders it barred.
func (g *Gate) Exclusive(ctx context.Context) (release func(), err error) {
	g.mu.Lock()
	g.queued++
	for g.held || g.shared > 0 {
		if err := g.wait(ctx); err != nil {
			g.mu.Lock()
			g.queued--
			g.broadcast()
			g.mu.Unlock()
			return nil, err
		}
	}
	g.queued--
	if err := ctx.Err(); err != nil {
		g.broadcast()
		g.mu.Unlock()
		return nil, err
	}
	g.held = true
	g.mu.Unlock()
	return g.release(func() { g.held = false }), nil
}

// wait releases g.mu until the gate's state next changes or ctx ends. It
// returns holding g.mu when the state changed, and without it when ctx
// ended, with ctx's error.
func (g *Gate) wait(ctx context.Context) error {
	if g.released == nil {
		g.released = make(chan struct{})
	}
	changed := g.released
	g.mu.Unlock()
	select {
	case <-changed:
		g.mu.Lock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// broadcast wakes every waiter to look at the gate again. The caller holds
// g.mu.
func (g *Gate) broadcast() {
	if g.released != nil {
		close(g.released)
		g.released = nil
	}
}

// release returns a release that applies undo once, under g.mu, and wakes
// the waiters.
func (g *Gate) release(undo func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			undo()
			g.broadcast()
			g.mu.Unlock()
		})
	}
}
