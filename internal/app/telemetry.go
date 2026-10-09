package app

import (
	"context"
	"log/slog"
	"maps"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-observability/otlp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/standards-lab/go-web-service/internal/config"
)

// serviceName is the service.name resource attribute every signal carries.
// compose/observability/otel-collector.yaml hardcodes the same value for the
// collector's log correlation, so the two must match character for
// character.
const serviceName = "go-web-service"

// telemetryFlushTimeout bounds the final flush of spans and metrics at
// shutdown. A reachable collector takes the flush in milliseconds; an
// unreachable one would otherwise hold the drain until the shutdown timeout.
const telemetryFlushTimeout = time.Second

// serviceVersion reports the service.version resource attribute from the
// binary's build information: the module version when the build has one, the
// VCS revision when it does not, and "dev" when neither is stamped.
func serviceVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			return s.Value
		}
	}
	return "dev"
}

// observabilityConfig returns cfg's observability block with the service's
// own identity, service.name and service.version, set over the configured
// resource attributes. The map is copied, not mutated: cfg is finalized, and
// nothing in this package mutates a finalized config. Every consumer of the
// telemetry configuration in this package reads it through this function so
// the identity attributes are set in one place.
func observabilityConfig(cfg *config.Config) observability.Config {
	obsCfg := cfg.Observability
	attrs := maps.Clone(obsCfg.ResourceAttributes)
	if attrs == nil {
		attrs = make(map[string]string, 2)
	}
	attrs["service.name"] = serviceName
	attrs["service.version"] = serviceVersion()
	obsCfg.ResourceAttributes = attrs
	return obsCfg
}

// defineTelemetry defines the telemetry node on g into n. It constructs
// nothing.
//
// The database and the object store order themselves after telemetry, so
// its Start installs the providers before either starts and its Shutdown
// flushes after both have closed. Those edges only order; the router's
// middleware stack, whose tracing records to the providers, Uses it, which
// is what brings it into the Build. The server sits above both
// connections, so every request is served while the providers are
// installed.
func defineTelemetry(g *graph.Graph, n *Nodes) {
	n.Telemetry = g.Define("telemetry", newTelemetry(n))
}

// newTelemetry constructs the telemetry service over the OTLP exporters.
// Construction performs no I/O: both exporters dial lazily.
func newTelemetry(n *Nodes) func(*graph.Scope) (*Telemetry, error) {
	return func(s *graph.Scope) (*Telemetry, error) {
		obsCfg := observabilityConfig(s.Use(n.Config))
		ctx := context.Background()

		traceExp, err := otlp.NewTraceExporter(ctx, obsCfg)
		if err != nil {
			return nil, err
		}
		metricExp, err := otlp.NewMetricExporter(ctx, obsCfg)
		if err != nil {
			return nil, err
		}
		reader := sdkmetric.NewPeriodicReader(metricExp)

		return &Telemetry{
			tel:    observability.New(obsCfg, observability.Exporters{Trace: traceExp, Metric: reader}),
			logger: s.Use(n.Logger),
		}, nil
	}
}

// Telemetry is the telemetry node's value: go-observability's Telemetry
// as a lifecycle participant, a Starter and a Stopper whose Shutdown never
// fails the run.
type Telemetry struct {
	tel     *observability.Telemetry
	logger  *slog.Logger
	started atomic.Bool
}

// Start installs the trace and meter providers.
func (t *Telemetry) Start(ctx context.Context) error {
	if err := t.tel.Start(ctx); err != nil {
		return err
	}
	t.started.Store(true)
	return nil
}

// Shutdown flushes and shuts down the providers, and returns nil whatever
// the flush does. It does nothing when Start did not succeed: the
// lifecycle shuts down a participant whose Start failed, and
// go-observability's Telemetry.Shutdown dereferences the providers only its
// Start sets.
//
// The flush fails whenever the collector is unreachable: the normal case in
// the integration tier, which never starts the observability compose
// profile. A returned flush error would fail the run and exit the process
// with code 1, turning an observability outage into a service outage, which
// is what this design exists to prevent; Shutdown logs the error at warn
// and swallows it. The flush is also bounded by telemetryFlushTimeout, well
// under the shutdown timeout: the OTLP exporters retry a refused export
// with a five-second initial backoff until their context ends, so an
// unbounded flush against an unreachable collector would hold the drain
// for the whole shutdown timeout on every exit.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if !t.started.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, telemetryFlushTimeout)
	defer cancel()
	if err := t.tel.Shutdown(ctx); err != nil {
		t.logger.Warn("telemetry shutdown", "error", err)
	}
	return nil
}
