package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// This file pins the graph's structural properties the composition root
// owns: the server alone in the top layer, the readiness checks the
// lifecycle infers from the node values and their order, telemetry beneath
// every other participant, the schema between the connections and the
// reactors, the relay held off by the gate, and each participant held by
// one node. It builds the
// graph without starting it, so it needs no live engine. Unlike
// app_test.go, these tests name nodes: they reach the graph through
// App.Graph and App.Nodes, as does the Build-failure case, which
// substitutes a failing constructor.

// buildAsRun builds a's graph from the roots Run builds from, the config,
// the logger, the server, the schema, and the reactors, plus extra.
// TestGraph_ServerAloneInTopLayer holds these roots to Run's by observing a
// real Run.
func buildAsRun(t *testing.T, a *app.App, extra ...graph.Ref) *graph.System {
	t.Helper()
	n := a.Nodes()
	roots := append([]graph.Ref{n.Config, n.Logger, n.Server, n.Schema}, n.Reactors...)
	sys, err := a.Graph().Build(append(roots, extra...)...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sys
}

// serverAloneOnTop reports whether the System's last layer holds the server
// node alone, and returns that layer.
func serverAloneOnTop(sys *graph.System, n app.Nodes) ([]graph.Dependency, bool) {
	layers := sys.Layers()
	if len(layers) == 0 {
		return nil, false
	}
	top := layers[len(layers)-1]
	alone := len(top) == 1 &&
		top[0].Name == n.Server.Name() &&
		top[0].Value == any(sys.Get(n.Server))
	return top, alone
}

// layerOf returns the index of the layer holding the node named name, or -1.
func layerOf(sys *graph.System, name string) int {
	for i, layer := range sys.Layers() {
		for _, d := range layer {
			if d.Name == name {
				return i
			}
		}
	}
	return -1
}

// names lists the layer's node names.
func names(layer []graph.Dependency) []string {
	out := make([]string, len(layer))
	for i, d := range layer {
		out[i] = d.Name
	}
	return out
}

// The server is the only node in the top layer of the graph Run builds, so
// it starts after every other node and drains first. A node defined above
// it, or beside it at the top, fails here; the second case shows the check
// catches one.
func TestGraph_ServerAloneInTopLayer(t *testing.T) {
	t.Run("as defined", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)

		// Run's own Build, observed, so the Build below inspects the nodes
		// Run reaches rather than a set this test chose. The context ends
		// before startup, so the Run starts nothing.
		ran := map[string]bool{}
		a.Graph().Observe(func(name string) { ran[name] = true })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if code := a.Run(ctx); code != 0 {
			t.Fatalf("Run = %d, want 0", code)
		}
		ranNames := slices.Sorted(maps.Keys(ran))

		sys := buildAsRun(t, a)
		var built []string
		for _, layer := range sys.Layers() {
			built = append(built, names(layer)...)
		}
		if slices.Sort(built); !slices.Equal(built, ranNames) {
			t.Fatalf("buildAsRun built %v, Run built %v; their roots differ", built, ranNames)
		}
		if top, ok := serverAloneOnTop(sys, a.Nodes()); !ok {
			t.Errorf("top layer = %+v, want the server node alone", names(top))
		}
	})

	t.Run("a node above the server", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		n := a.Nodes()
		above := a.Graph().Define("above", func(s *graph.Scope) (*web.Server, error) {
			return s.Use(n.Server), nil
		})
		sys := buildAsRun(t, a, above)
		if top, ok := serverAloneOnTop(sys, n); ok {
			t.Errorf("top layer = %+v, reported the server alone with a node above it", names(top))
		}
	})
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

// beneathTelemetry returns the lifecycle participants, other than telemetry
// itself, whose layer is not above telemetry's.
func beneathTelemetry(t *testing.T, sys *graph.System, n app.Nodes) []string {
	t.Helper()
	tel := layerOf(sys, n.Telemetry.Name())
	if tel < 0 {
		t.Fatal("telemetry is not in the built System")
	}
	var out []string
	for i, layer := range sys.Layers() {
		for _, d := range layer {
			if d.Name != n.Telemetry.Name() && participant(d.Value) && i <= tel {
				out = append(out, d.Name)
			}
		}
	}
	return out
}

// lateStarter is a participant that orders itself after nothing.
type lateStarter struct{}

func (lateStarter) Start(context.Context) error { return nil }

// Telemetry sits beneath every other lifecycle participant, so its
// providers are installed before any starts and flushed after all have
// stopped. A participant that misses the edge to telemetry fails here; the
// second case shows the check catches one.
func TestGraph_TelemetryBeneathParticipants(t *testing.T) {
	t.Run("as defined", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		sys := buildAsRun(t, a)
		if below := beneathTelemetry(t, sys, a.Nodes()); len(below) > 0 {
			t.Errorf("participants %v are not above telemetry's layer", below)
		}
	})

	t.Run("a participant missing the edge", func(t *testing.T) {
		a := app.New(configtest.Config(t), io.Discard)
		stray := a.Graph().Define("stray", func(*graph.Scope) (lateStarter, error) {
			return lateStarter{}, nil
		})
		sys := buildAsRun(t, a, stray)
		if below := beneathTelemetry(t, sys, a.Nodes()); !slices.Contains(below, "stray") {
			t.Errorf("participants below telemetry = %v, want stray among them", below)
		}
	})
}

// The connections share a layer beneath the schema, so they start together
// and close together after it, and the sweeper sits above the schema, so it
// starts once every table it touches is verified and stops before the
// schema does.
func TestGraph_SchemaBetweenConnectionsAndSweeper(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	sys := buildAsRun(t, a)
	n := a.Nodes()

	db, store := layerOf(sys, n.Database.Name()), layerOf(sys, n.Storage.Name())
	schema, sweeper := layerOf(sys, n.Schema.Name()), layerOf(sys, n.Sweeper.Name())
	if db < 0 || store < 0 || schema < 0 || sweeper < 0 {
		t.Fatalf("layers: database %d, storage %d, schema %d, sweeper %d; want each built", db, store, schema, sweeper)
	}
	if db != store {
		t.Errorf("database in layer %d, storage in layer %d, want the same layer", db, store)
	}
	if db >= schema {
		t.Errorf("connections in layer %d, schema in layer %d, want the connections below", db, schema)
	}
	if sweeper <= schema {
		t.Errorf("sweeper in layer %d, schema in layer %d, want the sweeper above", sweeper, schema)
	}
}

// The broker shares the connections' layer, and the relay sits above both
// it and the schema: the relay starts once the messaging set is migrated
// and verified and the broker is connected, and it drains, its last pass
// included, before the schema, the broker, and the database stop. The
// document node does not Use the relay, so the schema, which Uses the
// document node for its seed, can sit beneath it.
func TestGraph_RelayAboveSchemaAndBroker(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	sys := buildAsRun(t, a)
	n := a.Nodes()

	db, broker := layerOf(sys, n.Database.Name()), layerOf(sys, n.Broker.Name())
	schema, relay := layerOf(sys, n.Schema.Name()), layerOf(sys, n.Relay.Name())
	if db < 0 || broker < 0 || schema < 0 || relay < 0 {
		t.Fatalf("layers: database %d, broker %d, schema %d, relay %d; want each built", db, broker, schema, relay)
	}
	if broker != db {
		t.Errorf("broker in layer %d, database in layer %d, want the same layer", broker, db)
	}
	if relay <= schema {
		t.Errorf("relay in layer %d, schema in layer %d, want the relay above", relay, schema)
	}
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
	port := silentPort(t)
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
	// and its claim dials the silent database; the pause lets it reach the
	// dial before the drain begins. A pause that fell short would leave no
	// cycle in flight, and the drain's nil fails the test rather than
	// passing it.
	deadline := time.Now().Add(5 * time.Second)
	for !sweeper.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("the sweeper never became ready")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sweeper.Shutdown(ctx)
	if err == nil || !strings.Contains(err.Error(), "handlers cancelled after 100ms") {
		t.Errorf("Shutdown = %v, want the cycle cancelled after 100ms, half the shutdown timeout", err)
	}
}

// silentPort listens on a loopback port that accepts each connection and
// never answers it, so a client's handshake waits until its context ends.
// The listener and its connections close at cleanup.
func silentPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
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
	return lis.Addr().(*net.TCPAddr).Port
}

// The nudge the document layer gives after each branch it marks reaches
// the sweeper's files reclaimer: in the graph Run builds, the nudge node is
// bound to the sweeper, which panics on a nudge of a name it was not built
// with, so a nudge that reached no reclaimer, or the wrong one, fails here.
// Unbound, as in a graph built without the sweeper, a nudge does nothing.
func TestGraph_TheDocumentNudgeReachesTheFilesReclaimer(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	n := a.Nodes()

	alone, err := a.Graph().Build(n.Document)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	alone.Get(n.Nudge).Nudge()

	sys := buildAsRun(t, a)
	if layerOf(sys, n.Nudge.Name()) >= layerOf(sys, n.Document.Name()) {
		t.Errorf("nudge in layer %d, document in layer %d; want the nudge beneath the document it serves",
			layerOf(sys, n.Nudge.Name()), layerOf(sys, n.Document.Name()))
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("the nudge panicked, reaching no registered reclaimer: %v", r)
		}
	}()
	sys.Get(n.Nudge).Nudge()
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
	for _, layer := range sys.Layers() {
		for _, d := range layer {
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

// A constructor's error fails Run's Build: Run returns 1 and logs the
// failure, labelled with the failing node's name, through a logger of its
// own, since the node that failed may be the logger itself. Nothing reports
// ready.
func TestRun_BuildFailureExitsOne(t *testing.T) {
	var log syncBuffer
	a := app.New(configtest.Config(t), &log)
	a.Graph().Replace(a.Nodes().Logger, func(*graph.Scope) (*slog.Logger, error) {
		return nil, errors.New("logger unavailable")
	})

	// Cancelled up front, so a Build that wrongly succeeds drains at once
	// and fails the exit-code check instead of hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx); code != 1 {
		t.Errorf("Run = %d, want 1", code)
	}
	out := log.String()
	failure := regexp.MustCompile(`msg="service failed" error="[^"]*logger[^"]*logger unavailable`)
	if !failure.MatchString(out) {
		t.Errorf("log = %q, want a service failure naming the logger node and its error", out)
	}
	if strings.Contains(out, "server ready") {
		t.Errorf("log = %q, want no ready record", out)
	}
}
