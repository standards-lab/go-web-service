package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database/sweep"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/standards-lab/go-web-service/internal/app"
	"github.com/standards-lab/go-web-service/internal/apptest"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// This file proves the sweeper's per-pass hook, the sweep metrics node's
// value, under a manual reader: the outcome each pass counts as, the
// backlog gauge per reclaimer, and the warn record of a failed pass. It
// builds the hook through the published graph (apptest.SweepHook), its
// meter provider node substituted (apptest.ManualMeter), and reports
// passes to it directly, so it needs no database and no exporter; the
// integration tier's sweep_metrics_test.go proves the same instruments
// reach the OTLP exporter from a running sweeper.

// meterScope is the instrumentation scope the service's own instruments
// are created under.
const meterScope = "github.com/standards-lab/go-web-service"

// sweepFixture is a sweep metrics hook built over a manual reader, and the
// log its logger writes, as JSON lines.
type sweepFixture struct {
	hook   sweep.Hook
	reader *sdkmetric.ManualReader
	log    *syncBuffer
}

// newSweepFixture builds the sweep metrics node of an App over a hermetic
// config, its logger writing JSON, and its meter provider replaced by one
// over a manual reader.
func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	cfg := configtest.Config(t)
	cfg.Log.Format = logging.FormatJSON
	log := &syncBuffer{}
	a := app.New(cfg, log)
	reader := apptest.ManualMeter(t, a)
	return &sweepFixture{hook: apptest.SweepHook(t, a), reader: reader, log: log}
}

// sweepPoints is one collection of the sweeper's instruments.
type sweepPoints struct {
	// passes is sweep.passes by "reclaimer/outcome".
	passes map[string]int64
	// backlogged is sweep.backlogged by reclaimer.
	backlogged map[string]int64
}

// collect reads the sweeper's two instruments from f's reader, failing t
// when either has the wrong kind or unit.
func (f *sweepFixture) collect(t *testing.T) sweepPoints {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := sweepPoints{passes: map[string]int64{}, backlogged: map[string]int64{}}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != meterScope {
			t.Errorf("scope %q, want only %q", sm.Scope.Name, meterScope)
			continue
		}
		for _, m := range sm.Metrics {
			switch m.Name {
			case "sweep.passes":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || !sum.IsMonotonic || m.Unit != "{pass}" {
					t.Fatalf("sweep.passes is %T (monotonic %v), unit %q; want a monotonic int64 sum in {pass}", m.Data, sum.IsMonotonic, m.Unit)
				}
				for _, p := range sum.DataPoints {
					got.passes[value(p.Attributes, "sweep.reclaimer")+"/"+value(p.Attributes, "sweep.outcome")] = p.Value
				}
			case "sweep.backlogged":
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok || m.Unit != "1" {
					t.Fatalf("sweep.backlogged is %T, unit %q; want an int64 gauge in 1", m.Data, m.Unit)
				}
				for _, p := range gauge.DataPoints {
					got.backlogged[value(p.Attributes, "sweep.reclaimer")] = p.Value
				}
			default:
				t.Errorf("unexpected metric %q", m.Name)
			}
		}
	}
	return got
}

// value returns the string attribute key of set, or "" when it has none.
func value(set attribute.Set, key attribute.Key) string {
	v, _ := set.Value(key)
	return v.AsString()
}

// warnRecord is the fields a "sweep pass failed" record carries.
type warnRecord struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Reclaimer string `json:"reclaimer"`
	More      bool   `json:"more"`
	Duration  int64  `json:"duration"`
	Error     string `json:"error"`
}

// failures returns the "sweep pass failed" records f's log holds.
func (f *sweepFixture) failures(t *testing.T) []warnRecord {
	t.Helper()
	var out []warnRecord
	sc := bufio.NewScanner(strings.NewReader(f.log.String()))
	for sc.Scan() {
		var r warnRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		if r.Msg == "sweep pass failed" {
			out = append(out, r)
		}
	}
	return out
}

// Each pass counts once, under its reclaimer and its outcome: error when
// it returned an error, whatever it reported of more, so error wins over
// more; more when it reported more; drained otherwise.
func TestSweepMetrics_Outcomes(t *testing.T) {
	failed := errors.New("reclaim failed")
	tests := []struct {
		name string
		pass sweep.Pass
		want string
	}{
		{"drained", sweep.Pass{Name: "files"}, "files/drained"},
		{"more", sweep.Pass{Name: "outbox", More: true}, "outbox/more"},
		{"error", sweep.Pass{Name: "inbox", Err: failed}, "inbox/error"},
		{"error wins over more", sweep.Pass{Name: "files", More: true, Err: failed}, "files/error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSweepFixture(t)
			f.hook(context.Background(), tt.pass)
			if got, want := f.collect(t).passes, map[string]int64{tt.want: 1}; !maps.Equal(got, want) {
				t.Errorf("sweep.passes = %v, want %v", got, want)
			}
		})
	}
}

// The counter accumulates per reclaimer and outcome.
func TestSweepMetrics_PassesAccumulate(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()
	f.hook(ctx, sweep.Pass{Name: "files", More: true})
	f.hook(ctx, sweep.Pass{Name: "files", More: true})
	f.hook(ctx, sweep.Pass{Name: "files"})
	f.hook(ctx, sweep.Pass{Name: "outbox"})

	want := map[string]int64{"files/more": 2, "files/drained": 1, "outbox/drained": 1}
	if got := f.collect(t).passes; !maps.Equal(got, want) {
		t.Errorf("sweep.passes = %v, want %v", got, want)
	}
}

// sweep.backlogged reports each reclaimer the sweeper registers at 0 from
// the start, 1 once its last pass reported more, and 0 again once one
// drained, each reclaimer independently of the others. A failed pass that
// reported more leaves its reclaimer backlogged.
func TestSweepMetrics_Backlogged(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()
	steps := []struct {
		name string
		pass *sweep.Pass
		want map[string]int64
	}{
		{"from the start", nil, map[string]int64{"files": 0, "outbox": 0, "inbox": 0}},
		{"files more", &sweep.Pass{Name: "files", More: true}, map[string]int64{"files": 1, "outbox": 0, "inbox": 0}},
		{"outbox more", &sweep.Pass{Name: "outbox", More: true}, map[string]int64{"files": 1, "outbox": 1, "inbox": 0}},
		{"files drained", &sweep.Pass{Name: "files"}, map[string]int64{"files": 0, "outbox": 1, "inbox": 0}},
		{"inbox failed with more", &sweep.Pass{Name: "inbox", More: true, Err: errors.New("x")}, map[string]int64{"files": 0, "outbox": 1, "inbox": 1}},
		{"outbox drained", &sweep.Pass{Name: "outbox"}, map[string]int64{"files": 0, "outbox": 0, "inbox": 1}},
	}
	for _, s := range steps {
		if s.pass != nil {
			f.hook(ctx, *s.pass)
		}
		if got := f.collect(t).backlogged; !maps.Equal(got, s.want) {
			t.Errorf("after %s: sweep.backlogged = %v, want %v", s.name, got, s.want)
		}
	}
}

// A failed pass is logged at warn as "sweep pass failed", with its
// reclaimer, whether it reported more, its duration, and its error, as
// go-database's default report, which the hook replaces, logs it. A pass
// that succeeded, drained or with more, logs nothing.
func TestSweepMetrics_WarnOnFailedPass(t *testing.T) {
	f := newSweepFixture(t)
	ctx := context.Background()
	f.hook(ctx, sweep.Pass{Name: "files", Duration: time.Millisecond})
	f.hook(ctx, sweep.Pass{Name: "outbox", More: true, Duration: time.Millisecond})
	if got := f.failures(t); len(got) != 0 {
		t.Fatalf("after successful passes, failure records = %+v, want none", got)
	}

	f.hook(ctx, sweep.Pass{Name: "inbox", More: true, Duration: 2 * time.Second, Err: errors.New("reclaim failed")})
	want := []warnRecord{{
		Level: "WARN", Msg: "sweep pass failed", Reclaimer: "inbox", More: true,
		Duration: int64(2 * time.Second), Error: "reclaim failed",
	}}
	got := f.failures(t)
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("failure records = %+v, want %+v", got, want)
	}
}
