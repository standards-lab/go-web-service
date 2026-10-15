package app

import (
	"sync/atomic"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/go-messaging"
	msgpg "github.com/standards-lab/go-messaging/postgres"
)

// defineReactors defines the reactors on g into n: the entry points that
// run for the process lifetime, driven by an occurrence rather than a
// caller. The sweeper is one: go-database's general sweeper, running each
// reclaimer the service registers (files, outbox, and inbox), and the nudge
// the document layer reaches its files reclaimer through. The messaging is
// the other: the runtime, and the relay it builds, which publishes the
// outbox's committed events. No node Uses a reactor, so each is listed in
// n.Reactors, which Run builds as roots and the server orders itself after.
// It constructs nothing.
//
// The relay's last pass publishes what was committed while the producers
// drained, so every producer must drain before it. A node that commits
// events outside a request, such as a consumer's reactor, Uses the relay's
// node, which orders it above the relay. A request-driven producer, as
// every domain service is, needs no edge: its commands run in the server's
// requests, and the server, ordered after every reactor, drains first.
func defineReactors(g *graph.Graph, n *Nodes) {
	n.Nudge = g.Define("nudge", func(*graph.Scope) (*Nudge, error) { return new(Nudge), nil })
	n.Sweeper = g.Define("sweeper", newSweeper(n))
	n.Runtime = g.Define("runtime", newRuntime(n))
	n.Relay = g.Define("relay", newRelay(n))
	n.Reactors = []graph.Ref{n.Sweeper, n.Relay}
}

// The names the sweeper registers its reclaimers under: the name a nudge
// takes, and the lock "sweep.<name>" its cycle claims. reclaimFiles is
// blobfs's sweep, reclaimOutbox and reclaimInbox go-messaging's retention
// of the outbox and the inbox.
const (
	reclaimFiles  = "files"
	reclaimOutbox = "outbox"
	reclaimInbox  = "inbox"
)

// reclaimers names every reclaimer the sweeper registers, in its
// registration order: the sweep metrics node reports each one's backlog
// from the start, before its first pass. A reclaimer newSweeper registers
// is named here too.
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

// newSweeper constructs go-database's sweeper over the sql node's session,
// registering three reclaimers, each every interval of the config node's
// sweep block and in passes of its batch: files, the files node's blobfs
// sweep, reclaiming the file rows older than the block's stale age; outbox,
// go-messaging's outbox retention, deleting the published outbox rows older
// than its outbox age; and inbox, the inbox retention, deleting the handled
// inbox rows older than its inbox age. A retention that refuses its age is a
// construction error, so it fails startup, though the config's validation
// refuses a non-positive age first. Only files is nudged, by the document
// layer; the retentions run at start and on the interval. The interval is the
// backstop for a nudge that never came and the schedule of the stale rows'
// reclaim; every reclaimer is due at start, so a branch marked by a process
// that stopped before its sweep ran is swept at this process's start rather
// than an interval later. Each pass holds the gate shared, so a schema verb
// never meets one. The dialect is Postgres, a sqlate.TryLocker, so each
// cycle claims "sweep.<name>" (data.LockSweepPrefix) and two replicas never
// run one reclaimer's cycle at once. Each pass is reported to the sweep
// metrics node's hook (telemetry.go), which counts it and records its
// reclaimer's backlog on the meter provider node's provider, and logs a
// failed pass at warn as go-database's default report did. It binds the
// nudge node to the sweeper.
//
// Its value is a lifecycle Subsystem, ReadinessChecker, and Monitored, so
// the Coordinator starts it, drains it, reports it, and ends the run on its
// failure with no registration here. It orders itself after the schema, so
// it starts once every table it touches is verified, and stops before the
// schema's layer beneath it. Given the shutdown timeout, it cancels a pass
// still running at half of it (go-core's reactor.CancelWithin), so the
// sweeper reports the cancellation before the coordinator's deadline drops
// it.
func newSweeper(n *Nodes) func(*graph.Scope) (*sweep.Sweeper, error) {
	return func(s *graph.Scope) (*sweep.Sweeper, error) {
		s.After(n.Schema)
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

		policy := sweep.Policy{Interval: cfg.Sweep.Interval.Duration(), Budget: *cfg.Sweep.Batch}
		regs := []sweep.Registration{
			{Name: reclaimFiles, Reclaimer: s.Use(n.Files).FilesReclaimer(db, cfg.Sweep.StaleAge.Duration(), logger), Policy: policy},
			{Name: reclaimOutbox, Reclaimer: outbox, Policy: policy},
			{Name: reclaimInbox, Reclaimer: inbox, Policy: policy},
		}
		sw, err := sweep.New(db, regs, cfg.ShutdownTimeout.Duration(), logger,
			sweep.Gated(s.Use(n.Gate)), sweep.OnPass(s.Use(n.SweepMetrics)))
		if err != nil {
			return nil, err
		}
		s.Use(n.Nudge).sweeper.Store(sw)
		return sw, nil
	}
}

// newRuntime constructs the service's messaging from the config node's
// messaging block, over the broker node and go-messaging's Postgres engine
// statements, logging failures through the logger node. Its reactors drain
// within the shutdown timeout and hold the gate shared, so a schema verb,
// which holds it exclusively, runs with them paused: the relay opens no
// transaction and claims no row until the verb releases it. The runtime
// holds no resource and registers nothing, so it is no lifecycle
// participant; the broker beneath it and the reactors it builds are.
func newRuntime(n *Nodes) func(*graph.Scope) (*messaging.Runtime, error) {
	return func(s *graph.Scope) (*messaging.Runtime, error) {
		cfg := s.Use(n.Config)
		return messaging.New(cfg.Messaging, s.Use(n.Broker), msgpg.Outbox(), msgpg.Inbox(),
			cfg.ShutdownTimeout.Duration(), s.Use(n.Logger), messaging.Gated(s.Use(n.Gate)))
	}
}

// newRelay constructs the relay over the sql node's session. Its value is a
// lifecycle Subsystem, ReadinessChecker, and Monitored, as the sweeper
// is. It orders itself after the schema, so its first pass finds the
// messaging set migrated and verified. The runtime gives it go-messaging's
// drain: a last pass bounded at a quarter of the shutdown timeout, and the
// reactor's cancellation of a publish still running at half of it. It drains
// before the broker beneath it, which it Uses through the runtime, so the
// last pass publishes before the connection closes.
func newRelay(n *Nodes) func(*graph.Scope) (*reactor.Reactor[event.Event], error) {
	return func(s *graph.Scope) (*reactor.Reactor[event.Event], error) {
		s.After(n.Schema)
		return s.Use(n.Runtime).Relay(s.Use(n.SQL).DB), nil
	}
}
