// Package events is the events command family: it watches the events the
// service publishes to its NATS JetStream stream, live, and prints each
// one. The service publishes every event with its type as the subject under
// its prefix, such as web-service.document.file.uploaded on the stream
// WEB_SERVICE; a watch reads that stream and settles nothing, so it is an
// observer the service never sees.
//
// It connects through go-messaging's NATS broker as a non-owner, which
// binds the stream the service provisioned and writes nothing, and consumes
// through the broker's native connection with JetStream's ordered
// consumer, ephemeral and unacknowledged, which the watch deletes when it
// ends. Each message decodes with go-core's event.Decode, its NATS headers
// taken as the event's, as go-messaging's own NATS source takes them.
//
// The package exports:
//
//   - [Broker] and [NewBroker], the broker node's value: the non-owner
//     broker, a lifecycle Starter whose failure names the mise task that
//     fixes it, and [Broker.Watch], the live read of its stream
//   - [Decode], a message's event
//   - [Render], an event as one block of output
//   - [Commands], which builds the events command and its watch leaf on
//     go-cli-sdk over the broker node
//
// broker.go holds the broker, watch.go the watch and the decode, print.go
// the rendering, and commands.go the command.
package events
