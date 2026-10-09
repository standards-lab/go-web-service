package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

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
// sweeper, and each participant held by one node. It builds the
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

// The readiness checks the lifecycle infers from the node values are the
// database, the object store, the schema, and the sweeper, in that order;
// /readyz reports them after "lifecycle". No other value has a Ready: not
// the sweep's wake, whose source's readiness the sweeper reports, and not
// the readiness node itself.
func TestGraph_ReadinessChecks(t *testing.T) {
	cfg := configtest.Config(t)
	a := app.New(cfg, io.Discard)
	sys := buildAsRun(t, a)

	var got []string
	for _, c := range lifecycle.New(sys, cfg.Config).Checks() {
		got = append(got, c.Name)
	}
	want := []string{"database", "storage", "schema", "sweeper"}
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

// Each lifecycle participant is the value of one node only: the same value
// in two nodes would start twice, stop twice, and, as a ReadinessChecker,
// be reported twice. The admin storage routes read the storage node's
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

// Telemetry's Shutdown does nothing when its Start never ran, as the
// lifecycle's shutdown of a participant whose Start failed calls it: it
// returns nil, logs nothing, and does not reach go-observability's
// providers, which only Start sets.
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
