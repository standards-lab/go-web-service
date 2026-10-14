package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/standards-lab/go-core/event"
)

// cleanupWait bounds the stop and the consumer's removal once the watch's
// context has ended.
const cleanupWait = 5 * time.Second

// inactiveThreshold is how long the server keeps the watch's consumer after
// its last pull. The watch removes the consumer itself when it returns; the
// threshold is the backstop for a process killed before it can, shorter
// than the ordered consumer's five-minute default so a killed watcher's
// consumer does not linger. A running watch pulls continually, well inside
// it.
const inactiveThreshold = time.Minute

// Watch delivers to show each event the stream holds under the broker's
// prefix, in stream order, until ctx ends, and returns nil then. fromStart
// replays every event the stream holds first and then follows; otherwise
// only the events published after the watch starts are delivered.
//
// It consumes through the broker's native connection with JetStream's
// ordered consumer: ephemeral, unacknowledged, and recreated by the client
// from the last sequence it saw whenever it falls out of order or the
// connection returns, so the watch reads the stream without settling
// anything and leaves the service's own consumers untouched. When ctx ends
// it stops consuming and deletes the consumer, so the watch leaves no
// consumer on the server.
//
// A message that does not decode as an event is reported to report and
// skipped, as is any error the consumer survives, such as missed
// heartbeats while the server is away. show and report are called from the
// consumer's one delivery goroutine, never concurrently.
func (b *Broker) Watch(ctx context.Context, fromStart bool, show func(event.Event), report func(error)) error {
	js, err := jetstream.New(b.Conn())
	if err != nil {
		return fmt.Errorf("nats: %w", err)
	}
	deliver := jetstream.DeliverNewPolicy
	if fromStart {
		deliver = jetstream.DeliverAllPolicy
	}
	cons, err := js.OrderedConsumer(ctx, b.Stream(), jetstream.OrderedConsumerConfig{
		FilterSubjects:    []string{b.Filter()},
		DeliverPolicy:     deliver,
		InactiveThreshold: inactiveThreshold,
	})
	if err != nil {
		return fmt.Errorf("nats: watch stream %s: %w", b.Stream(), err)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		e, err := Decode(msg.Headers(), msg.Data())
		if err != nil {
			report(fmt.Errorf("message on %s: %w", msg.Subject(), err))
			return
		}
		show(e)
	}, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		report(err)
	}))
	if err != nil {
		return fmt.Errorf("nats: watch stream %s: %w", b.Stream(), err)
	}
	<-ctx.Done()
	return b.stop(context.WithoutCancel(ctx), js, cons, cc)
}

// stop stops consuming, waits up to cleanupWait for the delivery to finish,
// and deletes the ordered consumer's current server-side consumer. The
// client stops but never deletes it, and an earlier one, replaced on a
// reset, the client deletes itself. A consumer already gone, as after the
// server's restart, is no error.
func (b *Broker) stop(ctx context.Context, js jetstream.JetStream, cons jetstream.Consumer, cc jetstream.ConsumeContext) error {
	ctx, cancel := context.WithTimeout(ctx, cleanupWait)
	defer cancel()
	cc.Stop()
	select {
	case <-cc.Closed():
	case <-ctx.Done():
	}
	info := cons.CachedInfo()
	if info == nil {
		return nil
	}
	err := js.DeleteConsumer(ctx, b.Stream(), info.Name)
	if err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) {
		return fmt.Errorf("nats: delete consumer %s: %w", info.Name, err)
	}
	return nil
}

// Decode reads the event a message carries in binary content mode. A NATS
// message's headers are the event's headers as they are, the conversion
// go-messaging's NATS source makes, and headers that are not the event's,
// such as Nats-Msg-Id, are ignored.
func Decode(h natsgo.Header, data []byte) (event.Event, error) {
	return event.Decode(event.Header(h), data)
}
