package app

import (
	"context"
	"fmt"
	"maps"
	"runtime/debug"
	"time"

	"github.com/standards-lab/go-core/lifecycle"
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

// newTelemetry constructs the telemetry service over the OTLP exporters and
// registers its start and shutdown on lc as hooks rather than as a staged
// service: the startup hook runs before the first numbered stage and the
// shutdown hook after the last, so telemetry brackets every stage without
// holding a stage number of its own. Construction performs no I/O: both
// exporters dial lazily.
func newTelemetry(infra *Infrastructure, cfg *config.Config, lc *lifecycle.Coordinator) error {
	obsCfg := observabilityConfig(cfg)
	ctx := context.Background()

	traceExp, err := otlp.NewTraceExporter(ctx, obsCfg)
	if err != nil {
		return fmt.Errorf("telemetry: trace exporter: %w", err)
	}
	metricExp, err := otlp.NewMetricExporter(ctx, obsCfg)
	if err != nil {
		return fmt.Errorf("telemetry: metric exporter: %w", err)
	}
	reader := sdkmetric.NewPeriodicReader(metricExp)

	tel := observability.New(obsCfg, observability.Exporters{Trace: traceExp, Metric: reader})

	lc.OnStartup(func(ctx context.Context) error {
		if err := tel.Start(ctx); err != nil {
			return fmt.Errorf("telemetry: %w", err)
		}
		return nil
	})

	// The shutdown hook never returns an error. Telemetry.Shutdown
	// force-flushes both providers over OTLP, which fails whenever the
	// collector is unreachable: the normal case in the integration tier,
	// which never starts the observability compose profile. A returned
	// flush error would join Coordinator.Run's return and exit the process
	// with code 1, turning an observability outage into a service outage,
	// which is what this design exists to prevent. The hook logs the error
	// and swallows it.
	//
	// The flush is also bounded by telemetryFlushTimeout, well under the
	// drain timeout. The OTLP exporters retry a refused export with a
	// five-second initial backoff until their context ends, so an unbounded
	// flush against an unreachable collector would hold the drain for the
	// whole shutdown timeout on every exit.
	lc.OnShutdown(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, telemetryFlushTimeout)
		defer cancel()
		if err := tel.Shutdown(ctx); err != nil {
			infra.Logger.Warn("telemetry shutdown", "error", err)
		}
		return nil
	})

	return nil
}
