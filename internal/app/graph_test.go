package app_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/graph/graphtest"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// This file pins the graph's structural properties the composition root
// owns: each node named by its Nodes field, the root set App.Roots
// publishes building what a real Run builds, the server alone in the top
// layer, the reactors group's members, the readiness checks the lifecycle
// infers from the node values and their order, telemetry beneath every
// other participant, the connections in one layer and the schema between
// them and the reactors, the root set dropping no order, the relay held
// off by the gate, and each participant held by one node. It builds the
// graph without starting it, so it needs no live engine. Unlike
// app_test.go, these tests name nodes: they reach the graph through
// App.Graph and App.Nodes, as does the Build-failure case, which
// substitutes a failing constructor. They read placement by handle: the
// System's layer of each node handle, whether it holds a handle, and the
// handle on each built node.
//
// Each placement guard a fixture can break is a function returning an
// error that carries the System's layer table, called both by the test of
// the App's own roots and by a test of a fixture that breaks it, so the
// fixture shows the real assertion can fail.

// Each node's name is its Nodes field's name lowercased, ignoring the
// name's spaces, so a field names the node it holds without a comment
// repeating the name, and every field holds a handle once New returns.
func TestGraph_NodesNamedByTheirFields(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	graphtest.CheckNames(t, a.Nodes())
}

// runSet returns the set Run builds, App.Roots plus the lifecycle config
// node, plus extra. TestGraph_RootsBuildAsRun holds this set to Run's by
// observing a real Run.
func runSet(a *app.App, extra ...graph.Ref) []graph.Ref {
	return append(append(a.Roots(), a.Nodes().LifecycleConfig), extra...)
}

// buildAsRun builds a's graph from runSet(a, extra...).
func buildAsRun(t *testing.T, a *app.App, extra ...graph.Ref) *graph.System {
	t.Helper()
	sys, err := a.Graph().Build(runSet(a, extra...)...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sys
}

// observeRun runs a with a context already ended, so the Run builds and
// starts nothing, and returns the handle of every node its Build
// constructed.
func observeRun(t *testing.T, a *app.App) []graph.Ref {
	t.Helper()
	var ran []graph.Ref
	a.Graph().Observe(func(r graph.Ref) { ran = append(ran, r) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx, io.Discard); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	return ran
}

// App.Roots is the server and the reactors group, and a Build of it plus
// the lifecycle config node constructs exactly the nodes a real Run
// constructs, compared by handle: every node Run built is in the System,
// and the System holds no node Run did not build. It holds the tests
// below, which inspect that set, to what Run reaches.
func TestGraph_RootsBuildAsRun(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	n := a.Nodes()
	if got, want := a.Roots(), []graph.Ref{n.Server, n.Reactors}; !slices.Equal(got, want) {
		t.Errorf("Roots = %v, want the server and the reactors group", got)
	}

	ran := observeRun(t, a)
	sys := buildAsRun(t, a)
	for _, r := range ran {
		if !sys.Has(r) {
			t.Errorf("Run built %s, the root set's Build did not", r.Name())
		}
	}
	for _, d := range sys.Dependencies() {
		if !slices.Contains(ran, d.Ref) {
			t.Errorf("the root set's Build built %s, Run did not", d.Name)
		}
	}
	if t.Failed() {
		t.Logf("layers:\n%s", sys)
	}
}

// layerError returns an error of the formatted message followed by sys's
// layer table.
func layerError(sys *graph.System, format string, args ...any) error {
	return fmt.Errorf(format+"\nlayers:\n%s", append(args, sys)...)
}

// wantGuardFails fails t unless err, a guard's verdict on sys, is an error
// that names mention and carries sys's layer table.
func wantGuardFails(t *testing.T, err error, sys *graph.System, mention string) {
	t.Helper()
	switch {
	case err == nil:
		t.Fatalf("the guard passed a fixture that breaks it\nlayers:\n%s", sys)
	case !strings.Contains(err.Error(), mention):
		t.Errorf("guard error = %q, want it to name %s", err, mention)
	case !strings.Contains(err.Error(), "\nlayers:\n"+sys.String()):
		t.Errorf("guard error = %q, want it to carry the layer table:\n%s", err, sys)
	}
}

// serverAlone reports, as an error carrying the layer table, any node of
// sys at or above the server's layer: the server must be alone in the top
// layer, so it starts after every other node and drains first.
func serverAlone(sys *graph.System, server graph.Ref) error {
	if !sys.Has(server) {
		return layerError(sys, "%s is not in the System", server.Name())
	}
	top := sys.LayerOf(server)
	var beside []string
	for _, d := range sys.Dependencies() {
		if d.Ref != server && sys.LayerOf(d.Ref) >= top {
			beside = append(beside, d.Name)
		}
	}
	if len(beside) > 0 {
		return layerError(sys, "%v at or above %s's layer %d; want %s alone in the top layer",
			beside, server.Name(), top, server.Name())
	}
	return nil
}

// The server is the only node in the top layer of the graph Run builds, so
// it starts after every other node and drains first. A node defined above
// it, or beside it at the top, fails the guard; the second case shows the
// guard catches one.
func TestGraph_ServerAloneInTopLayer(t *testing.T) {
	t.Run("as defined", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		if err := serverAlone(buildAsRun(t, a), a.Nodes().Server); err != nil {
			t.Error(err)
		}
	})

	t.Run("a node above the server", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		n := a.Nodes()
		above := a.Graph().Define("above", func(s *graph.Scope) (*web.Server, error) {
			return s.Use(n.Server), nil
		})
		sys := buildAsRun(t, a, above)
		wantGuardFails(t, serverAlone(sys, n.Server), sys, "above")
	})
}

// The reactors group's members are the sweeper and the relay, each joined
// by its graph.In alone, and each sits below the server. go-core lists no
// group's members, so they are read from Builds: a Build of the group
// holds both, and holds no node a Build of the two does not, so a third
// member would show unless the two already reach it. A Build of the
// server and the lifecycle config node, Run's roots without the group,
// holds neither, so no Use reaches them and the group alone puts them in
// Run's Build.
func TestGraph_ReactorsGroupMembers(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	n := a.Nodes()
	reactors := []graph.Ref{n.Sweeper, n.Relay}
	build := func(roots ...graph.Ref) *graph.System {
		t.Helper()
		sys, err := a.Graph().Build(roots...)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return sys
	}

	group := build(n.Reactors)
	for _, r := range reactors {
		if !group.Has(r) {
			t.Errorf("a Build of %s does not hold %s; want it a member", n.Reactors.Name(), r.Name())
		}
	}
	pair := build(reactors...)
	for _, d := range group.Dependencies() {
		if !pair.Has(d.Ref) {
			t.Errorf("a Build of %s holds %s, which the sweeper and the relay do not reach; want no other member",
				n.Reactors.Name(), d.Name)
		}
	}

	ungrouped := build(n.Server, n.LifecycleConfig)
	for _, r := range reactors {
		if ungrouped.Has(r) {
			t.Errorf("a Build of the roots without %s holds %s; want only the group to reach it",
				n.Reactors.Name(), r.Name())
		}
	}

	sys := buildAsRun(t, a)
	for _, r := range reactors {
		if sys.LayerOf(r) >= sys.LayerOf(n.Server) {
			t.Errorf("%s in layer %d, server in layer %d; want it below the server",
				r.Name(), sys.LayerOf(r), sys.LayerOf(n.Server))
		}
	}
	if t.Failed() {
		t.Logf("layers:\n%s", sys)
	}
}

// The readiness checks the lifecycle infers from the node values, each one
// that both starts and reports Ready, are the database, the object store,
// the broker, the schema, the sweeper, and the relay, in that order;
// /readyz reports them after "lifecycle". No other value is both: not the
// nudge the document layer reaches the sweeper through, not the
// messaging runtime, which starts nothing, and not the readiness node
// itself.
func TestGraph_ReadinessChecks(t *testing.T) {
	cfg := configtest.Config(t)
	a := app.New(cfg, io.Discard)
	sys := buildAsRun(t, a)

	var got []string
	for _, c := range lifecycle.New(sys, cfg.Config).Checks() {
		got = append(got, c.Name)
	}
	want := []string{"database", "storage", "broker", "schema", "sweeper", "relay"}
	if !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
}

// participant reports whether v takes part in the lifecycle's startup or
// shutdown.
func participant(v any) bool {
	switch v.(type) {
	case lifecycle.Starter, lifecycle.Stopper:
		return true
	}
	return false
}

// telemetryBeneath reports, as an error carrying the layer table, a
// System without telemetry, or any lifecycle participant other than
// telemetry whose layer is not above telemetry's. Telemetry's presence is
// checked first, so a System missing it cannot pass with no participant
// compared.
func telemetryBeneath(sys *graph.System, telemetry graph.Ref) error {
	if !sys.Has(telemetry) {
		return layerError(sys, "%s is not in the System", telemetry.Name())
	}
	floor := sys.LayerOf(telemetry)
	var below []string
	for _, d := range sys.Dependencies() {
		if d.Ref != telemetry && participant(d.Value) && sys.LayerOf(d.Ref) <= floor {
			below = append(below, d.Name)
		}
	}
	if len(below) > 0 {
		return layerError(sys, "participants %v are not above %s's layer %d", below, telemetry.Name(), floor)
	}
	return nil
}

// lateStarter is a participant that orders itself after nothing.
type lateStarter struct{}

func (lateStarter) Start(context.Context) error { return nil }

// Telemetry sits beneath every other lifecycle participant, so its
// providers are installed before any starts and flushed after all have
// stopped. A participant that misses the edge to telemetry fails the
// guard; the second case shows the guard catches one.
func TestGraph_TelemetryBeneathParticipants(t *testing.T) {
	t.Run("as defined", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		if err := telemetryBeneath(buildAsRun(t, a), a.Nodes().Telemetry); err != nil {
			t.Error(err)
		}
	})

	t.Run("a participant missing the edge", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		stray := a.Graph().Define("stray", func(*graph.Scope) (lateStarter, error) {
			return lateStarter{}, nil
		})
		sys := buildAsRun(t, a, stray)
		wantGuardFails(t, telemetryBeneath(sys, a.Nodes().Telemetry), sys, "stray")
	})
}

// The database, the object store, and the broker share a layer, so they
// start together, concurrently, and close together. The schema sits above
// them, so it starts once they are connected and stops before they close,
// and below the sweeper and the relay, so they start once every table they
// touch is verified and stop before the schema does. The relay also sits
// above the broker, so it drains, its last pass included, before the
// broker stops. The document node does not Use the relay, so the schema,
// which Uses the document node for its seed, can sit beneath it.
func TestGraph_SchemaBetweenConnectionsAndReactors(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	sys := buildAsRun(t, a)
	n := a.Nodes()

	for _, r := range []graph.Ref{n.Database, n.Storage, n.Broker, n.Schema, n.Sweeper, n.Relay} {
		if !sys.Has(r) {
			t.Fatalf("%s is not in the System\nlayers:\n%s", r.Name(), sys)
		}
	}
	for _, conn := range []graph.Ref{n.Storage, n.Broker} {
		if d, c := sys.LayerOf(n.Database), sys.LayerOf(conn); d != c {
			t.Errorf("%s in layer %d, %s in layer %d; want the same layer", n.Database.Name(), d, conn.Name(), c)
		}
	}
	below := func(lower, upper graph.Ref) {
		t.Helper()
		if l, u := sys.LayerOf(lower), sys.LayerOf(upper); l >= u {
			t.Errorf("%s in layer %d, %s in layer %d; want %s below", lower.Name(), l, upper.Name(), u, lower.Name())
		}
	}
	for _, conn := range []graph.Ref{n.Database, n.Storage, n.Broker} {
		below(conn, n.Schema)
	}
	below(n.Schema, n.Sweeper)
	below(n.Schema, n.Relay)
	below(n.Broker, n.Relay)
	if t.Failed() {
		t.Logf("layers:\n%s", sys)
	}
}

// rootDrops builds roots from a's graph and reports, as an error carrying
// the layer table, a failed Build or any After the Build dropped. The
// reactors group has members, so the server's After of it orders; any
// drop means a root is missing from the set, or a node orders itself
// after one no root reaches, so an order is silently lost.
func rootDrops(a *app.App, roots []graph.Ref) error {
	sys, err := a.Graph().Build(roots...)
	if err != nil {
		return fmt.Errorf("Build: %w", err)
	}
	var drops []string
	for _, d := range sys.Dropped() {
		drops = append(drops, fmt.Sprintf("%s's After of %s", d.Node.Name(), d.After.Name()))
	}
	if len(drops) > 0 {
		return layerError(sys, "dropped %s; want every After to order", strings.Join(drops, ", "))
	}
	return nil
}

// A Build of the root set drops no After. The second case shows the guard
// catches an After whose target the roots do not build.
func TestGraph_RootsDropNothing(t *testing.T) {
	t.Run("as defined", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		if err := rootDrops(a, runSet(a)); err != nil {
			t.Error(err)
		}
	})

	t.Run("an After of a node not built", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		unbuilt := a.Graph().Value("unbuilt", 0)
		orderer := a.Graph().Value("orderer", 0, graph.After(unbuilt))
		roots := runSet(a, orderer)
		sys, err := a.Graph().Build(roots...)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		wantGuardFails(t, rootDrops(a, roots), sys, "orderer's After of unbuilt")
	})
}

// The relay holds the gate shared for each row, so while a schema verb
// holds it exclusively the relay opens no transaction: against a database
// that refuses every connection, it reports no failure until the gate is
// released, and then reports the refused connection. No live engine is
// needed: the failure the release lets through is the refusal itself.
func TestGraph_RelayHeldOffByTheGate(t *testing.T) {
	cfg := configtest.Config(t)
	cfg.Messaging.RelayPoll = libconfig.Duration(5 * time.Millisecond)
	var log syncBuffer
	a := app.New(cfg, &log)
	n := a.Nodes()
	sys, err := a.Graph().Build(n.Relay)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	failed := func() bool { return strings.Contains(log.String(), `msg="messaging failure"`) }

	release, err := sys.Get(n.Gate).Exclusive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	relay := sys.Get(n.Relay)
	if err := relay.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Many polls' worth: a relay that ignored the gate would have dialled
	// the closed port and reported the refusal by now.
	time.Sleep(100 * time.Millisecond)
	if failed() {
		t.Fatalf("the relay reported a failure while the gate was held exclusively:\n%s", log.String())
	}

	release()
	deadline := time.Now().Add(5 * time.Second)
	for !failed() {
		if time.Now().After(deadline) {
			t.Fatalf("no relay failure after the gate's release; want the refused connection reported:\n%s", log.String())
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := relay.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown = %v, want nil", err)
	}
}

// The sweeper cancels a cycle still running half the shutdown timeout into
// its drain, go-core's CancelWithin rule, and its drain reports the period:
// under the 10s default, "handlers cancelled after 5s". A shorter timeout
// keeps the test fast. The cycle in flight is the files reclaimer's, its
// claim waiting on a connection to a database that accepts and never
// answers, which only the cancellation ends, so no live engine is needed.
// The drain alone does not end it: a claim, unlike a wait on the gate,
// runs until its context does.
func TestGraph_SweeperCancelsWithinTheShutdownTimeout(t *testing.T) {
	cfg := configtest.Config(t)
	cfg.ShutdownTimeout = libconfig.Duration(200 * time.Millisecond)
	port, dialled := silentPort(t)
	cfg.Database.Port = &port
	a := app.New(cfg, io.Discard)
	n := a.Nodes()
	sys, err := a.Graph().Build(n.Sweeper)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sweeper := sys.Get(n.Sweeper)
	if err := sweeper.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Every reclaimer is due at start, so the files cycle begins at once
	// and its claim dials the silent database: once the dial is accepted,
	// the cycle is in flight, waiting on an answer that never comes.
	select {
	case <-dialled:
	case <-time.After(failsafe):
		t.Fatal("the sweeper's claim never dialled the database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sweeper.Shutdown(ctx)
	if err == nil || !strings.Contains(err.Error(), "handlers cancelled after 100ms") {
		t.Errorf("Shutdown = %v, want the cycle cancelled after 100ms, half the shutdown timeout", err)
	}
}

// silentPort listens on a loopback port that accepts each connection and
// never answers it, so a client's handshake waits until its context ends.
// The channel is closed once the first connection is accepted. The
// listener and its connections close at cleanup.
func silentPort(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan struct{})
	first := sync.OnceFunc(func() { close(accepted) })
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			first()
		}
	}()
	t.Cleanup(func() {
		_ = lis.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return lis.Addr().(*net.TCPAddr).Port, accepted
}

// closingPort listens on a loopback port that closes each connection as it
// accepts it, so a client's handshake fails at once. The listener closes at
// cleanup.
func closingPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = lis.Close() })
	return lis.Addr().(*net.TCPAddr).Port
}

// claimFailures counts the sweeper's records, in log, of a failed claim of
// the lock of the reclaimer named.
func claimFailures(log, reclaimer string) int {
	re := regexp.MustCompile(`sweep claim failed.*reclaimer\W+` + regexp.QuoteMeta(reclaimer) + `\b`)
	return len(re.FindAllStringIndex(log, -1))
}

// The nudge the document layer gives after each branch it marks reaches
// the sweeper's files reclaimer and no other. The database closes every
// connection, so each cycle's claim fails and is logged under its
// reclaimer's name; with an interval of an hour, a cycle after the start's
// runs only on a nudge. The nudge node the document layer is built with
// then earns the files reclaimer, alone, a second cycle: a nudge left
// unbound, or bound to another reclaimer, never logs one. The records also
// show the service's sweeper claims each reclaimer's lock: one built
// sweep.Unlocked would log no claim. Unbound, as in a graph built without
// the sweeper, a nudge does nothing and never panics.
func TestGraph_TheDocumentNudgeReachesTheFilesReclaimer(t *testing.T) {
	cfg := configtest.Config(t)
	cfg.Sweep.Interval = libconfig.Duration(time.Hour)
	port := closingPort(t)
	cfg.Database.Port = &port
	log := &syncBuffer{}
	a := app.New(cfg, log)
	n := a.Nodes()

	alone, err := a.Graph().Build(n.Document)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	alone.Get(n.Nudge).Nudge()

	sys, err := a.Graph().Build(n.Document, n.Sweeper)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sweeper := sys.Get(n.Sweeper)
	if err := sweeper.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sweeper.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	// The start's cycles run in the list's order, inbox last.
	processtest.WaitFor(t, "the start's cycles", func() bool {
		return claimFailures(log.String(), "inbox") == 1
	})

	sys.Get(n.Nudge).Nudge()
	processtest.WaitFor(t, "the nudged files cycle", func() bool {
		return claimFailures(log.String(), "files") == 2
	})
	if o, i := claimFailures(log.String(), "outbox"), claimFailures(log.String(), "inbox"); o != 1 || i != 1 {
		t.Errorf("outbox and inbox cycles = %d and %d, want the start's alone:\n%s", o, i, log)
	}
}

// The sweeper registers the three reclaimers the service runs, files,
// outbox, and inbox: go-database's Sweeper panics on a nudge of a name it
// was not built with, so a reclaimer missing from the registration, or
// registered under another name, fails here. A name it was not built with
// is refused, which shows a nudge of a missing name does fail.
func TestGraph_SweeperRegistersTheReclaimers(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	sweeper := buildAsRun(t, a).Get(a.Nodes().Sweeper)

	nudge := func(name string) (panicked bool) {
		defer func() { panicked = recover() != nil }()
		sweeper.Nudge(name)
		return false
	}
	for _, name := range []string{"files", "outbox", "inbox"} {
		if nudge(name) {
			t.Errorf("Nudge(%q) panicked; want %s registered", name, name)
		}
	}
	if !nudge("sessions") {
		t.Error(`Nudge("sessions") did not panic; want a name the sweeper was not built with refused`)
	}
}

// Each lifecycle participant is the value of one node only: the same value
// in two nodes would start twice, stop twice, and, as a ReadyStarter, be
// reported twice. The admin storage routes read the storage node's
// store rather than holding it as a node of their own.
func TestGraph_EachParticipantInOneNode(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	sys := buildAsRun(t, a)

	held := map[any]string{}
	for _, d := range sys.Dependencies() {
		switch d.Value.(type) {
		case lifecycle.Starter, lifecycle.Stopper, lifecycle.ReadinessChecker, lifecycle.Monitored:
		default:
			continue
		}
		if other, dup := held[d.Value]; dup {
			t.Errorf("nodes %s and %s hold the same participant", other, d.Name)
		}
		held[d.Value] = d.Name
	}
}

// Telemetry's Shutdown is safe when its Start never ran, as the
// lifecycle's shutdown of a participant whose Start failed calls it: it
// returns nil and logs nothing, go-observability releasing the exporters
// Start never handed to a provider.
func TestTelemetry_ShutdownWithoutStart(t *testing.T) {
	var log syncBuffer
	a := app.New(configtest.Config(t), &log)
	sys, err := a.Graph().Build(a.Nodes().Telemetry)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := sys.Get(a.Nodes().Telemetry).Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown = %v, want nil", err)
	}
	if out := log.String(); out != "" {
		t.Errorf("log = %q, want nothing", out)
	}
}

// A constructor's error fails Run's Build: Run returns 1 and writes the
// failure to stderr, labelled with the failing node's name. It logs
// nothing, since the node that failed may be the logger itself, and
// nothing reports ready.
func TestRun_BuildFailureExitsOne(t *testing.T) {
	var log, stderr syncBuffer
	a := app.New(configtest.Config(t), &log)
	a.Graph().Replace(a.Nodes().Logger, func(*graph.Scope) (*slog.Logger, error) {
		return nil, errors.New("unavailable")
	})

	// Cancelled up front, so a Build that wrongly succeeds drains at once
	// and fails the exit-code check instead of hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx, &stderr); code != 1 {
		t.Errorf("Run = %d, want 1", code)
	}
	failure := regexp.MustCompile(`^service failed: .*\blogger\b.*unavailable`)
	if got := stderr.String(); !failure.MatchString(got) {
		t.Errorf("stderr = %q, want a service failure naming the logger node and its error", got)
	}
	if got := log.String(); got != "" {
		t.Errorf("log = %q, want nothing logged", got)
	}
}
