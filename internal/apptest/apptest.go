package apptest

import (
	"context"
	"testing"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-database/sweep"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/standards-lab/go-web-service/internal/app"
)

// ManualMeter replaces a's meter provider node's constructor with one that
// returns an SDK meter provider over the returned manual reader, so every
// instrument a later Build creates on that node is collected by the
// reader's Collect, and nowhere else. Each Build constructs a fresh
// provider over the same reader; a reader serves one provider, so a test
// builds the node once. The provider is shut down when t ends. ManualMeter
// is a Replace, so it panics once a has run a Build.
func ManualMeter(t testing.TB, a *app.App) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	a.Graph().Replace(a.Nodes().MeterProvider, func(*graph.Scope) (metric.MeterProvider, error) {
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
		return mp, nil
	})
	return reader
}

// SweepHook builds a's graph from its sweep metrics node alone and returns
// the node's value: the hook the sweeper reports each pass to. The Build
// reaches the node's inputs, the meter provider, the logger, and the
// config, and nothing above them, so it does no I/O. It fails t when the
// Build fails. A Build freezes a's graph, so apply every Replace first.
func SweepHook(t testing.TB, a *app.App) sweep.Hook {
	t.Helper()
	sys, err := a.Graph().Build(a.Nodes().SweepMetrics)
	if err != nil {
		t.Fatalf("Build sweep metrics: %v", err)
	}
	return sys.Get(a.Nodes().SweepMetrics)
}
