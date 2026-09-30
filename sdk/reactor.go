package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// The reactor, bound for go-core as its own package beside lifecycle,
// joins one source of occurrences to one function for the process
// lifetime. It is a lifecycle component with the Start, Shutdown, and Ready
// methods other infrastructure exposes, plus Err for a failure while
// running. It knows nothing of the coordinator: the composition root
// registers it with lifecycle.Coordinator.Add at the stage it chooses and
// passes its Err to Coordinator.Monitor.
//
// Start detaches the reactor from the context it is given, so cancelling
// the run context at a signal does not interrupt handling before the
// reactor's stage drains. Shutdown drains in two phases: it stops the
// source from taking new occurrences and waits for the handling in flight,
// then, once the Grace period passes, cancels the handlers' contexts and
// waits for them to unwind. Without Grace, a handler's context is
// cancelled only when Shutdown's own context ends, which is the drain
// deadline. A handler that runs in several steps, such as a loop over
// bounded passes, reads Draining from its context to stop between steps
// once the drain begins, rather than run until the grace cancels it.

// Func handles one occurrence. Its context carries the source's values,
// any deadline the source set for the occurrence, and the reactor's drain
// signal, which [Draining] reads; it is otherwise cancelled only when the
// drain cancels the handlers.
type Func[T any] func(ctx context.Context, occ T) error

// drainingKey is the context key under which a reactor puts its drain
// signal on each handler's context.
type drainingKey struct{}

// Draining returns the drain signal of the reactor running the handler
// whose context is ctx: a channel closed once the reactor's Shutdown
// begins. The handling in flight is not interrupted by it; a handler that
// works in steps checks it between them and returns once it is closed,
// leaving what remains to the next start. A context no reactor gave has
// no signal, and Draining returns nil, which never closes.
func Draining(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(drainingKey{}).(chan struct{})
	return ch
}

// Source delivers occurrences to a Func.
//
// What a handler error means is the source's own rule. An interval
// ([Every], [Wake]) has nothing to redeliver, so an error ends it; a
// subscription redelivers the occurrence instead and keeps receiving. A
// source may bound each occurrence with a deadline on the context it
// passes to fn, such as a broker's acknowledgement wait; the reactor keeps
// that deadline while detaching the handler from the source's own
// cancellation.
type Source[T any] interface {
	// Receive delivers each occurrence to fn until ctx ends, then waits for
	// the handling in flight and returns nil. It returns an error when the
	// source fails, or when fn's error ends it.
	Receive(ctx context.Context, fn Func[T]) error
	// Ready reports whether the source is receiving.
	Ready() bool
}

// reactorState is a reactor's place in its single-use life.
type reactorState int

const (
	reactorIdle reactorState = iota
	reactorRunning
	reactorStopping
	reactorStopped
)

// Option configures a Reactor.
type Option func(*reactorOptions)

type reactorOptions struct {
	grace time.Duration
}

// Grace bounds how long Shutdown lets the handling in flight run before it
// cancels the handlers' contexts. Shutdown then waits for them to unwind,
// until its own context ends. Set it below the coordinator's drain
// timeout, so a reactor that had to cancel its handlers says so in Run's
// result; the coordinator drops any error that arrives after its own
// deadline. Without Grace, the handlers are cancelled only when Shutdown's
// context ends.
func Grace(d time.Duration) Option {
	return func(o *reactorOptions) { o.grace = d }
}

// Reactor runs a Source into a Func. It is single use: Start once,
// Shutdown once.
type Reactor[T any] struct {
	src  Source[T]
	fn   Func[T]
	opts reactorOptions

	mu       sync.Mutex
	state    reactorState
	stop     context.CancelFunc // ends Receive's context
	abort    context.CancelFunc // cancels handler contexts
	err      error              // Receive's result once stopping
	draining chan struct{}      // closed once Shutdown begins
	done     chan struct{}
	errs     chan error
}

// New returns a reactor that runs src into fn.
func New[T any](src Source[T], fn Func[T], opts ...Option) *Reactor[T] {
	r := &Reactor[T]{
		src:      src,
		fn:       fn,
		draining: make(chan struct{}),
		done:     make(chan struct{}),
		errs:     make(chan error, 1),
	}
	for _, opt := range opts {
		opt(&r.opts)
	}
	return r
}

// Start launches the source and returns. The reactor keeps ctx's values
// but not its cancellation; only Shutdown stops it.
func (r *Reactor[T]) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != reactorIdle {
		return errors.New("reactor: started twice")
	}
	base := context.WithoutCancel(ctx)
	recvCtx, stop := context.WithCancel(base)
	abortCtx, abort := context.WithCancel(base)
	r.stop, r.abort = stop, abort
	r.state = reactorRunning

	handle := func(ctx context.Context, occ T) error {
		// Stopping the source must not interrupt the handler, but a
		// deadline the source set for this occurrence still applies.
		var hctx context.Context
		var cancel context.CancelFunc
		if d, ok := ctx.Deadline(); ok {
			hctx, cancel = context.WithDeadline(context.WithoutCancel(ctx), d)
		} else {
			hctx, cancel = context.WithCancel(context.WithoutCancel(ctx))
		}
		defer cancel()
		defer context.AfterFunc(abortCtx, cancel)()
		return r.fn(context.WithValue(hctx, drainingKey{}, r.draining), occ)
	}

	go func() {
		err := r.src.Receive(recvCtx, handle)
		r.mu.Lock()
		if r.state == reactorStopping {
			r.err = err
		} else if err != nil {
			r.errs <- fmt.Errorf("reactor: %w", err)
		}
		r.state = reactorStopped
		r.mu.Unlock()
		close(r.errs)
		close(r.done)
	}()
	return nil
}

// Shutdown drains the reactor in two phases. It closes the drain signal
// ([Draining]), stops the source, and waits for the handling in flight;
// once the [Grace] period passes, it cancels the handlers' contexts and
// waits for them to unwind. It returns once Receive returns, or when ctx
// ends, which cancels the handlers and stops waiting. The error reports a
// failure sent on Err that nothing read, a grace that ran out, or ctx's
// error, or else Receive's error. Shutdown before Start retires the
// reactor, so it never starts, and a later Shutdown of a retired reactor
// returns nil.
func (r *Reactor[T]) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	switch r.state {
	case reactorIdle:
		r.state = reactorStopped
		close(r.draining)
		close(r.errs)
		close(r.done)
		r.mu.Unlock()
		return nil
	case reactorRunning:
		r.state = reactorStopping
		close(r.draining)
	case reactorStopped:
		if r.stop == nil {
			// Retired before Start: there is no source to stop.
			r.mu.Unlock()
			return nil
		}
	}
	r.mu.Unlock()

	r.stop()
	defer r.abort()

	var grace <-chan time.Time
	if r.opts.grace > 0 {
		t := time.NewTimer(r.opts.grace)
		defer t.Stop()
		grace = t.C
	}
	cancelled := false
	for {
		select {
		case <-grace:
			grace = nil
			select {
			case <-r.done:
				continue // the drain finished as the grace ran out
			default:
			}
			r.abort()
			cancelled = true
		case <-ctx.Done():
			return fmt.Errorf("reactor: drain: %w", ctx.Err())
		case <-r.done:
			// An error sent on Err after the coordinator stopped monitoring
			// has no other reader; report it here rather than lose it.
			if err, ok := <-r.errs; ok {
				return err
			}
			r.mu.Lock()
			err := r.err
			r.mu.Unlock()
			if cancelled {
				// A handler that unwinds returns its cancellation; that is
				// the expected outcome, not a second failure.
				if errors.Is(err, context.Canceled) {
					err = nil
				}
				return errors.Join(
					fmt.Errorf("reactor: handlers cancelled after grace %v", r.opts.grace),
					err,
				)
			}
			if err != nil {
				return fmt.Errorf("reactor: %w", err)
			}
			return nil
		}
	}
}

// Ready reports whether the reactor is running and its source is ready. It
// turns false once Shutdown begins.
func (r *Reactor[T]) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == reactorRunning && r.src.Ready()
}

// Err yields the error that ends the source while the reactor is running,
// then closes when the source returns. A composition root passes it to the
// coordinator's Monitor, so a dead reactor ends the process.
func (r *Reactor[T]) Err() <-chan error {
	return r.errs
}

// Every returns a source that delivers the time on every tick of d. The
// handler runs synchronously, and a tick that comes due while it runs is
// dropped, so the handler never receives a stale time. A handler error
// ends the source, and so the reactor, which reports it on Err; a handler
// that tolerates a failure returns nil. Every panics if d is not positive,
// as time.NewTicker does.
func Every(d time.Duration) Source[time.Time] {
	if d <= 0 {
		panic("reactor: Every needs a positive interval")
	}
	return &every{d: d}
}

type every struct {
	d     time.Duration
	ready atomic.Bool
}

func (s *every) Receive(ctx context.Context, fn Func[time.Time]) error {
	t := time.NewTicker(s.d)
	defer t.Stop()
	s.ready.Store(true)
	defer s.ready.Store(false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-t.C:
			if ctx.Err() != nil {
				return nil
			}
			if err := fn(ctx, now); err != nil {
				return err
			}
			select {
			case <-t.C: // drop the tick that came due during fn
			default:
			}
		}
	}
}

func (s *every) Ready() bool { return s.ready.Load() }

// Wake returns a source that delivers the time on every tick of d, as
// [Every] does, and also on a [Waker.Nudge]: the interval is the backstop
// and the nudge the prompt, for work whose producer can say that some is
// waiting. Nudges coalesce: any number before the next delivery produce
// one, and a nudge while the handler runs produces exactly one delivery
// after it. A tick that comes due while the handler runs is dropped, as
// Every drops it. A handler error ends the source, as it ends Every. Wake
// panics if d is not positive.
func Wake(d time.Duration) *Waker {
	if d <= 0 {
		panic("reactor: Wake needs a positive interval")
	}
	return &Waker{d: d, nudge: make(chan struct{}, 1)}
}

// Waker is the [Wake] source: a [Source] of times with a Nudge its
// producers call.
type Waker struct {
	d     time.Duration
	nudge chan struct{} // one pending nudge; a second coalesces into it
	ready atomic.Bool
}

// Nudge asks for a delivery as soon as the handler is free. It returns at
// once and never blocks: a nudge already pending absorbs it. A nudge
// before the source starts is delivered when it does; one after the
// source stops delivers nothing.
func (s *Waker) Nudge() {
	select {
	case s.nudge <- struct{}{}:
	default:
	}
}

// Receive delivers the time on each tick and each pending nudge until ctx
// ends.
func (s *Waker) Receive(ctx context.Context, fn Func[time.Time]) error {
	t := time.NewTicker(s.d)
	defer t.Stop()
	s.ready.Store(true)
	defer s.ready.Store(false)
	for {
		var now time.Time
		select {
		case <-ctx.Done():
			return nil
		case now = <-t.C:
		case <-s.nudge:
			now = time.Now()
		}
		if ctx.Err() != nil {
			return nil
		}
		if err := fn(ctx, now); err != nil {
			return err
		}
		select {
		case <-t.C: // drop the tick that came due during fn
		default:
		}
	}
}

// Ready reports whether the source is receiving.
func (s *Waker) Ready() bool { return s.ready.Load() }
