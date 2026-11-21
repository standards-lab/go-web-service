package app

import (
	"fmt"
	"sync/atomic"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/go-messaging"
	msgpg "github.com/standards-lab/go-messaging/postgres"
)

func defineReactors(g *graph.Graph, n *Nodes) {
	n.Reactors = g.Group("reactors")
	n.Nudge = g.Fresh[Nudge]("nudge")
	n.Sweeper = g.Define("sweeper", n.newSweeper, graph.In(n.Reactors), graph.After(n.Schema))
	n.Runtime = g.Define("runtime", n.newRuntime)
	n.Relay = g.Define("relay", n.newRelay, graph.In(n.Reactors), graph.After(n.Schema))
}

const (
	reclaimFiles  = "files"
	reclaimOutbox = "outbox"
	reclaimInbox  = "inbox"
)

var reclaimers = []string{reclaimFiles, reclaimOutbox, reclaimInbox}

// Nudge is the nudge node's value, the document layer's document.Sweeper:
// the layer nudges it after each branch it marks, and it marks the
// sweeper's files reclaimer due. It is a node of its own, bound by the
// sweeper's constructor, because the document layer cannot Use the
// sweeper: the schema Uses the domain nodes for their seeds, and the
// sweeper orders itself after the schema. It has no Ready, so the probe
// gains no "nudge" check.
type Nudge struct {
	sweeper atomic.Pointer[sweep.Sweeper]
}

// Nudge asks the sweeper for a pass of its files reclaimer as soon as it
// is free. Before the sweeper is built, or in a graph built without it, it
// does nothing: every reclaimer is due at the sweeper's start, so a branch
// marked before then is swept at once all the same.
func (n *Nudge) Nudge() {
	if s := n.sweeper.Load(); s != nil {
		s.Nudge(reclaimFiles)
	}
}

func (n *Nodes) newSweeper(s *graph.Scope) (*sweep.Sweeper, error) {
	cfg := s.Use(n.Config)
	db := s.Use(n.SQL).DB
	logger := s.Use(n.Logger)

	outbox, err := msgpg.OutboxRetention(db, cfg.Sweep.OutboxAge.Duration())
	if err != nil {
		return nil, err
	}
	inbox, err := msgpg.InboxRetention(db, cfg.Sweep.InboxAge.Duration())
	if err != nil {
		return nil, err
	}

	built := map[string]sweep.Reclaimer{
		reclaimFiles:  s.Use(n.Files).FilesReclaimer(db, cfg.Sweep.StaleAge.Duration(), logger),
		reclaimOutbox: outbox,
		reclaimInbox:  inbox,
	}
	regs, err := registrations(built, sweep.Policy{Interval: cfg.Sweep.Interval.Duration(), Budget: *cfg.Sweep.Batch})
	if err != nil {
		return nil, err
	}
	sw, err := sweep.New(db, regs, cfg.ShutdownTimeout.Duration(), logger,
		sweep.Options{Gate: s.Use(n.Gate), OnPass: s.Use(n.SweepMetrics)})
	if err != nil {
		return nil, err
	}
	s.Use(n.Nudge).sweeper.Store(sw)
	return sw, nil
}

func registrations(built map[string]sweep.Reclaimer, policy sweep.Policy) ([]sweep.Registration, error) {
	regs := make([]sweep.Registration, 0, len(reclaimers))
	for _, name := range reclaimers {
		r, ok := built[name]
		if !ok {
			return nil, fmt.Errorf("sweeper: no reclaimer for %q", name)
		}
		regs = append(regs, sweep.Registration{Name: name, Reclaimer: r, Policy: policy})
	}
	if len(built) != len(regs) {
		return nil, fmt.Errorf("sweeper: %d reclaimers built, %d named", len(built), len(regs))
	}
	return regs, nil
}

func (n *Nodes) newRuntime(s *graph.Scope) (*messaging.Runtime, error) {
	cfg := s.Use(n.Config)
	return messaging.Setup{
		Config: cfg.Messaging, Broker: s.Use(n.Broker), Outbox: msgpg.Outbox(), Inbox: msgpg.Inbox(),
		Shutdown: cfg.ShutdownTimeout.Duration(), Logger: s.Use(n.Logger), Gate: s.Use(n.Gate),
	}.Build()
}

func (n *Nodes) newRelay(s *graph.Scope) (*reactor.Reactor[event.Event], error) {
	return s.Use(n.Runtime).Relay(s.Use(n.SQL).DB), nil
}
