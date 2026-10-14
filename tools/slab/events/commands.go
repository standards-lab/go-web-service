package events

import (
	"context"
	"fmt"
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// commandPath prefixes every line the watch writes to stderr, as the
// dispatcher prefixes the report of a command's failure.
const commandPath = "slab events watch"

// Commands builds the events command, on go-cli-sdk, with its one leaf,
// watch, and returns it alone in the slice the composition root mounts
// with root.Add(Commands(...)...). The events command declares with Use
// out's color node and broker, the NATS broker bound to the service's
// stream, which the dispatcher starts before watch runs, so a watch begins
// only once NATS answers and the stream is there, and drains when it
// returns.
//
// watch takes no arguments. Its Run says on stderr what it watches, then
// renders each event through out, until the run's context ends on an
// interrupt, which is the watch's clean end: it returns nil, and the
// command exits 0. --from-start replays the stream first.
func Commands(broker *graph.Node[*Broker], out output.Source) []*cli.Command {
	var fromStart bool
	watch := &cli.Command{
		Name:    "watch",
		Summary: "Print each event the service publishes as it arrives, until interrupted",
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			b, o := inv.Get(broker), out.New(inv)
			report := func(err error) { _, _ = fmt.Fprintf(inv.Stderr, "%s: %v\n", commandPath, err) }
			announce(inv.Stderr, b, fromStart)
			return b.Watch(ctx, fromStart, func(e event.Event) {
				if err := o.Block(Render(o.Style(), e)); err != nil {
					report(err)
				}
			}, report)
		},
	}
	watch.Flags().BoolVar(&fromStart, "from-start", false, "replay every event the stream holds first, then follow")
	events := &cli.Command{
		Name:    "events",
		Summary: "Watch the events the service publishes",
	}
	events.Add(watch).Use(out.Node, broker)
	return []*cli.Command{events}
}

// announce writes the line a watch starts with, naming what it watches, on
// w, the invocation's stderr, so stdout carries the events alone.
func announce(w io.Writer, b *Broker, fromStart bool) {
	from := "events published from now on"
	if fromStart {
		from = "every event the stream holds, then each new one"
	}
	_, _ = fmt.Fprintf(w, "%s: watching stream %s on %s for %s (interrupt to stop)\n", commandPath, b.Stream(), b.Filter(), from)
}
