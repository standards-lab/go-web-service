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

func TestGraph_NodesNamedByTheirFields(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)
	graphtest.CheckNames(t, a.Nodes())
}

func runSet(a *app.App, extra ...graph.Ref) []graph.Ref {
	return append(append(a.Roots(), a.Nodes().LifecycleConfig), extra...)
}

func buildAsRun(t *testing.T, a *app.App, extra ...graph.Ref) *graph.System {
	t.Helper()
	sys, err := a.Graph().Build(runSet(a, extra...)...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return sys
}

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

func layerError(sys *graph.System, format string, args ...any) error {
	return fmt.Errorf(format+"\nlayers:\n%s", append(args, sys)...)
}

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

func participant(v any) bool {
	switch v.(type) {
	case lifecycle.Starter, lifecycle.Stopper:
		return true
	}
	return false
}

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

type lateStarter struct{}

func (lateStarter) Start(context.Context) error { return nil }

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

const manyRelayPolls = 100 * time.Millisecond

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
	time.Sleep(manyRelayPolls)
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

func claimFailures(log, reclaimer string) int {
	re := regexp.MustCompile(`sweep claim failed.*reclaimer\W+` + regexp.QuoteMeta(reclaimer) + `\b`)
	return len(re.FindAllStringIndex(log, -1))
}

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
	processtest.WaitFor(t, "the start's cycles, inbox last", func() bool {
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

func TestRun_BuildFailureExitsOne(t *testing.T) {
	var log, stderr syncBuffer
	a := app.New(configtest.Config(t), &log)
	a.Graph().Replace(a.Nodes().Logger, func(*graph.Scope) (*slog.Logger, error) {
		return nil, errors.New("unavailable")
	})

	endedBeforeStart, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(endedBeforeStart, &stderr); code != 1 {
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
