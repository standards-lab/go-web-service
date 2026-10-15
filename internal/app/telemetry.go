package app

import (
	"context"
	"log/slog"
	"maps"
	"runtime/debug"
	"sync"
	"time"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-observability/otlp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/standards-lab/go-web-service/internal/config"
)

// serviceName is the service.name resource attribute every signal carries.
// compose/otel-collector/otel-collector.yaml hardcodes the same value for the
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

// defineTelemetry defines the telemetry node, the meter provider node, and
// the sweep metrics node on g into n. It constructs nothing.
//
// The database and the object store order themselves after telemetry, so
// its Start installs the providers before either starts and its Shutdown
// flushes after both have closed. Those edges only order; the router's
// middleware stack, whose tracing records to the providers, Uses it, which
// is what brings it into the Build. The server sits above both
// connections, so every request is served while the providers are
// installed.
//
// The meter provider node is what the service's own instruments are
// created on, and the sweep metrics node is the sweeper's per-pass hook,
// which the sweeper Uses. Neither is a lifecycle participant.
func defineTelemetry(g *graph.Graph, n *Nodes) {
	n.Telemetry = g.Define("telemetry", newTelemetry(n))
	n.MeterProvider = g.Define("meterprovider", newMeterProvider)
	n.SweepMetrics = g.Define("sweepmetrics", newSweepMetrics(n))
}

// newMeterProvider returns the meter provider the service's own instruments
// are created on: the otel global, as the HTTP middleware's
// request-duration histogram resolves it. It is a node so a test can
// Replace it; the constructor installs nothing. Before the telemetry node's
// Start installs the real provider, the global is a delegating proxy that
// routes the instruments created on it to that provider once installed, so
// the node needs no edge to telemetry.
func newMeterProvider(*graph.Scope) (metric.MeterProvider, error) {
	return globalMeterProvider{}, nil
}

// globalMeterProvider is the meter provider node's value: it resolves the
// otel global at each Meter call rather than holding it. Once the
// telemetry node has started, the global is the SDK provider, a Stopper
// that node already shuts down; held as a node's value, it would be shut
// down by the lifecycle a second time.
type globalMeterProvider struct {
	embedded.MeterProvider
}

// Meter returns the global meter provider's Meter.
func (globalMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return otel.GetMeterProvider().Meter(name, opts...)
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
	tel    *observability.Telemetry
	logger *slog.Logger
}

// Start installs the trace and meter providers.
func (t *Telemetry) Start(ctx context.Context) error {
	return t.tel.Start(ctx)
}

// Shutdown flushes and shuts down the providers, and returns nil whatever
// the flush does. The lifecycle also shuts down a participant whose Start
// failed or never ran; go-observability's Telemetry.Shutdown then releases
// the exporters Start never handed to a provider, so nothing leaks.
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
	ctx, cancel := context.WithTimeout(ctx, telemetryFlushTimeout)
	defer cancel()
	if err := t.tel.Shutdown(ctx); err != nil {
		t.logger.Warn("telemetry shutdown", "error", err)
	}
	return nil
}

// meterName is the instrumentation scope of the service's own instruments:
// the module path, as an instrumentation library names its scope by its
// import path.
const meterName = "github.com/standards-lab/go-web-service"

// The sweep.outcome values of the sweep.passes counter.
const (
	outcomeError   = "error"
	outcomeMore    = "more"
	outcomeDrained = "drained"
)

// sweepMetrics records the sweeper's passes as two instruments, and logs
// the failed ones. Its hook replaces go-database's default report, so it
// carries that report too.
//
//   - sweep.passes, a counter, counts each pass by its sweep.reclaimer and
//     its sweep.outcome: error when the pass returned an error, whatever it
//     reported of more; more when it reported more; drained otherwise.
//   - sweep.backlogged, an observable gauge, reports per sweep.reclaimer 1
//     while that reclaimer's last pass reported more, and 0 otherwise, from
//     the start. A failed pass that reported more leaves it backlogged.
//
// The sweeper keeps no backlog state (go-database's sweep reports passes
// and nothing else), so the gauge's state is kept here, one value per
// reclaimer the sweeper registers, read at each collection.
type sweepMetrics struct {
	passes metric.Int64Counter
	logger *slog.Logger

	mu         sync.Mutex
	backlogged map[string]bool
}

// newSweepMetrics constructs the sweeper's per-pass hook: it creates the
// sweeper's instruments on the meter provider node's provider, for each
// reclaimer the sweeper registers, and returns the hook, which records each
// pass and logs a failed one at warn on the logger node's logger. The error
// is the meter's refusal of an instrument.
func newSweepMetrics(n *Nodes) func(*graph.Scope) (sweep.Hook, error) {
	return func(s *graph.Scope) (sweep.Hook, error) {
		meter := s.Use(n.MeterProvider).Meter(meterName)
		m := &sweepMetrics{logger: s.Use(n.Logger), backlogged: make(map[string]bool, len(reclaimers))}
		for _, name := range reclaimers {
			m.backlogged[name] = false
		}

		var err error
		m.passes, err = meter.Int64Counter("sweep.passes",
			metric.WithDescription("Sweeper passes run, by reclaimer and outcome."),
			metric.WithUnit("{pass}"))
		if err != nil {
			return nil, err
		}
		_, err = meter.Int64ObservableGauge("sweep.backlogged",
			metric.WithDescription("1 while the reclaimer's last pass reported more, else 0."),
			metric.WithUnit("1"),
			metric.WithInt64Callback(m.observe))
		if err != nil {
			return nil, err
		}
		return m.pass, nil
	}
}

// pass is the sweeper's hook: it counts p, records whether its reclaimer
// is backlogged, and logs p at warn when it failed.
func (m *sweepMetrics) pass(ctx context.Context, p sweep.Pass) {
	outcome := outcomeDrained
	switch {
	case p.Err != nil:
		outcome = outcomeError
	case p.More:
		outcome = outcomeMore
	}
	m.passes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("sweep.reclaimer", p.Name),
		attribute.String("sweep.outcome", outcome)))

	m.mu.Lock()
	m.backlogged[p.Name] = p.More
	m.mu.Unlock()

	// go-database's default report, which this hook replaces, word for
	// word: the integration tier counts these records.
	if p.Err != nil {
		m.logger.WarnContext(ctx, "sweep pass failed",
			"reclaimer", p.Name, "more", p.More, "duration", p.Duration, "error", p.Err)
	}
}

// observe reports each reclaimer's backlog to the sweep.backlogged gauge.
func (m *sweepMetrics) observe(_ context.Context, o metric.Int64Observer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, more := range m.backlogged {
		var v int64
		if more {
			v = 1
		}
		o.Observe(v, metric.WithAttributes(attribute.String("sweep.reclaimer", name)))
	}
	return nil
}
