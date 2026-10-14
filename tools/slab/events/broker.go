package events

import (
	"context"
	"errors"
	"fmt"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/standards-lab/go-messaging/nats"
)

// connectionName is the name slab's connection reports to the server, so a
// watcher is told apart from the service's own connection.
const connectionName = "slab"

// Broker is the broker node's value: go-messaging's NATS broker on the
// service's stream, as a non-owner. Its Start binds the stream as the
// service provisioned it and writes nothing, so a watcher never changes the
// stream's configuration; it fails when the stream is missing. The embedded
// broker's Shutdown drains the connection when the command returns, and
// Conn is the native handle [Broker.Watch] consumes through.
//
// It is a lifecycle Starter, as the scenarios' need nodes are: a command
// that declares the node runs only once NATS answers and the stream is
// there, and a failed Start names the mise task that fixes it.
type Broker struct {
	*nats.Broker
	cfg nats.Config
}

// NewBroker returns the broker on the NATS server at url, bound to stream
// under prefix, unconnected: it checks the configuration, as nats.New does,
// and does no I/O.
func NewBroker(url, stream, prefix string) (*Broker, error) {
	owner := false
	cfg := nats.Config{URL: url, Name: connectionName, Stream: stream, Prefix: prefix, Owner: &owner}
	b, err := nats.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Broker{Broker: b, cfg: cfg}, nil
}

// Start connects and binds the stream. Its error names the mise task that
// fixes what it found: nothing answering is the compose stack down, and a
// missing stream is the service never having started on this server,
// since the service is the stream's owner and creates it at its own start.
func (b *Broker) Start(ctx context.Context) error {
	err := b.Broker.Start(ctx)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		// The run ended during the connect, by a signal: the context's
		// error, which the lifecycle reports as no failure of its own.
		return ctx.Err()
	case errors.Is(err, natsgo.ErrNoServers):
		return fmt.Errorf("nothing answers at %s (start the stack with: mise run db:up): %w", b.cfg.URL, err)
	case errors.Is(err, jetstream.ErrStreamNotFound):
		return fmt.Errorf("NATS at %s has no stream %s, which the service creates when it starts (start the service with: mise run serve): %w", b.cfg.URL, b.cfg.Stream, err)
	default:
		return err
	}
}

// Stream is the stream the broker binds.
func (b *Broker) Stream() string { return b.cfg.Stream }

// Filter is the subject filter that captures every event type under the
// broker's prefix: Prefix.>, the stream's own subjects.
func (b *Broker) Filter() string { return b.cfg.Prefix + ".>" }
