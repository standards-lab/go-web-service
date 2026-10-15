package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-database/sweep"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/standards-lab/go-web-service/internal/app"
)

// sweepMeter builds the sweeper's hook over a meter provider read by a
// manual reader, for the reclaimers the service registers, logging to the
// returned buffer.
func sweepMeter(t *testing.T) (sweep.Hook, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	var log bytes.Buffer
	hook, err := app.NewSweepMetrics(mp, slog.New(slog.NewTextHandler(&log, nil)),
		[]string{"files", "outbox", "inbox"})
	if err != nil {
		t.Fatalf("NewSweepMetrics: %v", err)
	}
	return hook, reader, &log
}

// collect reads the reader's sweep.passes counter, as counts keyed by
// "reclaimer/outcome", and its sweep.backlogged gauge, keyed by reclaimer.
func collect(t *testing.T, reader *sdkmetric.ManualReader) (passes, backlogged map[string]int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	attr := func(s attribute.Set, key attribute.Key) string {
		v, _ := s.Value(key)
		return v.AsString()
	}
	passes, backlogged = map[string]int64{}, map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "sweep.passes":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || !sum.IsMonotonic {
					t.Fatalf("sweep.passes is %T, want a monotonic int64 sum", m.Data)
				}
				for _, p := range sum.DataPoints {
					passes[attr(p.Attributes, "sweep.reclaimer")+"/"+attr(p.Attributes, "sweep.outcome")] = p.Value
				}
			case "sweep.backlogged":
				g, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("sweep.backlogged is %T, want an int64 gauge", m.Data)
				}
				for _, p := range g.DataPoints {
					backlogged[attr(p.Attributes, "sweep.reclaimer")] = p.Value
				}
			}
		}
	}
	return passes, backlogged
}

// Each pass is counted once under its reclaimer and outcome: error when it
// returned an error, even one that reported more; more when it reported
// more; drained otherwise.
func TestSweepMetrics_CountsPassesByOutcome(t *testing.T) {
	hook, reader, _ := sweepMeter(t)
	ctx := context.Background()
	refused := errors.New("refused")

	hook(ctx, sweep.Pass{Name: "files", More: false})
	hook(ctx, sweep.Pass{Name: "outbox", More: true})
	hook(ctx, sweep.Pass{Name: "inbox", Err: refused})
	hook(ctx, sweep.Pass{Name: "files", More: true, Err: refused})
	hook(ctx, sweep.Pass{Name: "files", More: false})

	passes, _ := collect(t, reader)
	want := map[string]int64{
		"files/drained": 2,
		"files/error":   1,
		"outbox/more":   1,
		"inbox/error":   1,
	}
	if !maps.Equal(passes, want) {
		t.Errorf("sweep.passes = %v, want %v", passes, want)
	}
}

// The gauge reports every registered reclaimer from the start, 0 until a
// pass reports more; it goes to 1 after a pass that reported more, failed
// or not, and back to 0 after one that did not, each reclaimer on its own.
func TestSweepMetrics_GaugesBacklogPerReclaimer(t *testing.T) {
	hook, reader, _ := sweepMeter(t)
	ctx := context.Background()

	if _, got := collect(t, reader); !maps.Equal(got, map[string]int64{"files": 0, "outbox": 0, "inbox": 0}) {
		t.Errorf("before any pass, sweep.backlogged = %v, want every reclaimer 0", got)
	}

	hook(ctx, sweep.Pass{Name: "files", More: true})
	hook(ctx, sweep.Pass{Name: "inbox", More: true, Err: errors.New("refused")})
	if _, got := collect(t, reader); !maps.Equal(got, map[string]int64{"files": 1, "outbox": 0, "inbox": 1}) {
		t.Errorf("after files and inbox reported more, sweep.backlogged = %v, want files and inbox 1", got)
	}

	hook(ctx, sweep.Pass{Name: "files", More: false})
	if _, got := collect(t, reader); !maps.Equal(got, map[string]int64{"files": 0, "outbox": 0, "inbox": 1}) {
		t.Errorf("after files drained, sweep.backlogged = %v, want inbox alone 1", got)
	}
}

// A failed pass is logged at warn as go-database's default report logs it,
// the record the integration tier counts; a pass that succeeded is not
// logged.
func TestSweepMetrics_LogsAFailedPass(t *testing.T) {
	hook, _, log := sweepMeter(t)
	ctx := context.Background()

	hook(ctx, sweep.Pass{Name: "outbox", More: true, Duration: time.Millisecond})
	if log.Len() != 0 {
		t.Errorf("a pass that succeeded was logged:\n%s", log)
	}

	hook(ctx, sweep.Pass{Name: "files", More: true, Duration: time.Millisecond, Err: errors.New("refused")})
	want := `level=WARN msg="sweep pass failed" reclaimer=files more=true duration=1ms error=refused`
	if !strings.Contains(log.String(), want) {
		t.Errorf("failed pass logged as:\n%s\nwant a record containing:\n%s", log, want)
	}
}
