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

const serviceName = "go-web-service"

const telemetryFlushTimeout = time.Second

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

func defineTelemetry(g *graph.Graph, n *Nodes) {
	n.Telemetry = g.Define("telemetry", n.newTelemetry)
	n.MeterProvider = g.Define("meterprovider", newMeterProvider)
	n.SweepMetrics = g.Define("sweepmetrics", n.newSweepMetrics)
}

func newMeterProvider(*graph.Scope) (metric.MeterProvider, error) {
	return globalMeterProvider{}, nil
}

type globalMeterProvider struct {
	embedded.MeterProvider
}

// Meter returns the global meter provider's Meter.
func (globalMeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return otel.GetMeterProvider().Meter(name, opts...)
}

func (n *Nodes) newTelemetry(s *graph.Scope) (*Telemetry, error) {
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

const meterName = "github.com/standards-lab/go-web-service"

const (
	outcomeError   = "error"
	outcomeMore    = "more"
	outcomeDrained = "drained"
)

type sweepMetrics struct {
	passes metric.Int64Counter
	logger *slog.Logger

	mu         sync.Mutex
	backlogged map[string]bool
}

func (n *Nodes) newSweepMetrics(s *graph.Scope) (sweep.Hook, error) {
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

	if p.Err != nil {
		m.logger.WarnContext(ctx, "sweep pass failed",
			"reclaimer", p.Name, "more", p.More, "duration", p.Duration, "error", p.Err)
	}
}

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
