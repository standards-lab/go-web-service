package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-messaging"
	"github.com/standards-lab/go-messaging/nats"
)

// The stream config.json's nats block names and the prefix of the
// service's event types, which the harness's subscriber binds as it finds
// them.
const (
	eventStream = "WEB_SERVICE"
	eventPrefix = "web-service"
)

// Stream is a durable consumer of the service's stream, held by the
// harness, and the events it has received in their order.
type Stream struct {
	mu  sync.Mutex
	got []event.Event
}

// Subscribe binds the service's stream as a consumer, never its owner,
// through go-messaging's NATS broker, and returns once its durable
// consumer is created, so it receives every event of the given types the
// stream captures from then on, and none before. The durable is named for
// the test and the moment, so no two tests share one. The stream must
// exist: the service, its owner, creates it as it starts, so a test
// subscribes once a service has started. The broker is shut down at
// cleanup, after the consumer stops.
func Subscribe(t testing.TB, types ...string) *Stream {
	t.Helper()
	cfg := nats.Config{URL: brokerURL().String(), Name: "integration-harness", Stream: eventStream, Prefix: eventPrefix}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("broker config: %v", err)
	}
	b, err := nats.New(cfg)
	if err != nil {
		t.Fatalf("broker: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), processtest.Failsafe)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start the harness's broker on %s: %v", cfg.URL, err)
	}
	sub := messaging.Subscription{Name: durableName(t), Types: types, Start: messaging.StartNew}
	src, err := b.Subscribe(sub, func(_ context.Context, err error) { t.Logf("the harness's subscriber survived: %v", err) })
	if err != nil {
		t.Fatalf("subscribe %s: %v", sub.Name, err)
	}

	s := &Stream{}
	rctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- src.Receive(rctx, func(_ context.Context, e event.Event) error {
			s.mu.Lock()
			s.got = append(s.got, e)
			s.mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("the harness's subscriber: %v", err)
		}
		sctx, cancel := context.WithTimeout(context.Background(), processtest.Failsafe)
		defer cancel()
		_ = b.Shutdown(sctx)
	})
	deadline := time.Now().Add(processtest.Failsafe)
	for !src.Ready() {
		select {
		case err := <-done:
			t.Fatalf("the harness's subscriber ended before it was ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the harness's subscriber was not ready within %v", processtest.Failsafe)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s
}

// durableName is a consumer name unique to the test and the moment, made a
// token as a subscription's Name must be.
func durableName(t testing.TB) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, t.Name())
	return fmt.Sprintf("it_%s_%d", name, time.Now().UnixNano())
}

// Received is the events received so far, in their order.
func (s *Stream) Received() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.got...)
}

// Await waits until the stream has received an event whose subject is
// subject, and returns every event received by then, failing the test if
// none arrives within the failsafe. The relay publishes in commit order,
// so an event committed before the awaited one has arrived by then: a
// test that commits a marker after the commands it watches reads what
// those commands published from the events before the marker's.
func (s *Stream) Await(t testing.TB, subject string) []event.Event {
	t.Helper()
	var got []event.Event
	processtest.WaitFor(t, "the event about "+subject, func() bool {
		got = s.Received()
		for _, e := range got {
			if e.Subject == subject {
				return true
			}
		}
		return false
	})
	return got
}
