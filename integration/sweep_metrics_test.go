//go:build integration

package integration_test

import (
	"context"
	"maps"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"
	colmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"

	"github.com/standards-lab/go-web-service/integration"
)

// receiver is the metrics service of a minimal OTLP/gRPC collector, the
// service's exporter being gRPC alone (go-observability/otlp): it keeps the
// latest data point of the sweeper's two instruments, keyed by their
// attributes, and discards every other metric. The service's periodic
// reader exports cumulative sums, so a counter's latest point is its
// running total.
type receiver struct {
	colmetricpb.UnimplementedMetricsServiceServer

	mu sync.Mutex
	// passes is sweep.passes by "reclaimer/outcome".
	passes map[string]int64
	// backlogged is sweep.backlogged by reclaimer.
	backlogged map[string]int64
}

// startReceiver serves a receiver on a loopback port until the test ends
// and returns it with its address, host:port, the endpoint the service
// exports to.
func startReceiver(t *testing.T) (*receiver, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r := &receiver{passes: map[string]int64{}, backlogged: map[string]int64{}}
	srv := grpc.NewServer()
	colmetricpb.RegisterMetricsServiceServer(srv, r)
	coltracepb.RegisterTraceServiceServer(srv, traceSink{})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return r, l.Addr().String()
}

// Export records the sweeper's points of one export.
func (r *receiver) Export(_ context.Context, req *colmetricpb.ExportMetricsServiceRequest) (*colmetricpb.ExportMetricsServiceResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				switch m.GetName() {
				case "sweep.passes":
					for _, p := range m.GetSum().GetDataPoints() {
						r.passes[attr(p.GetAttributes(), "sweep.reclaimer")+"/"+attr(p.GetAttributes(), "sweep.outcome")] = p.GetAsInt()
					}
				case "sweep.backlogged":
					for _, p := range m.GetGauge().GetDataPoints() {
						r.backlogged[attr(p.GetAttributes(), "sweep.reclaimer")] = p.GetAsInt()
					}
				}
			}
		}
	}
	return &colmetricpb.ExportMetricsServiceResponse{}, nil
}

// traceSink is the collector's trace service: it accepts the service's
// spans and discards them, so its trace exporter logs no refusal.
type traceSink struct {
	coltracepb.UnimplementedTraceServiceServer
}

// Export discards one export's spans.
func (traceSink) Export(context.Context, *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// attr is the string value of the attribute key in attrs, or empty.
func attr(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// snapshot copies the latest points received.
func (r *receiver) snapshot() (passes, backlogged map[string]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.passes), maps.Clone(r.backlogged)
}

// The sweeper's metrics reach the collector through the service's own OTLP
// exporter: sweep.passes counts each reclaimer's passes by outcome, and
// sweep.backlogged reports each reclaimer. The service exports every 100ms
// (OTEL_METRIC_EXPORT_INTERVAL, which the SDK's periodic reader honors) to a
// receiver in the test, and sweeps at an interval of an hour, so every pass
// after the startup cycle is a delete's nudge, and a count that rises after
// a delete is that delete's. Its object store is relayed through a
// forwarder the test severs to have a files pass refused.
//
// A backlog of 1 is not asserted: a cycle runs its passes while one
// reports more, and a refused files pass reports none, so no reclaimer
// rests backlogged between collections, and a point of 1 could be caught
// only by racing an export against a pass.
func TestSweepMetrics(t *testing.T) {
	r, endpoint := startReceiver(t)
	f := processtest.Forward(t, integration.StorageAddr())
	s := integration.Start(t, integration.Options{
		Seed:    integration.Default,
		Storage: f.Addr(),
		Env: []string{
			"APP_OBSERVABILITY_ENDPOINT=" + endpoint,
			"OTEL_METRIC_EXPORT_INTERVAL=100",
			"APP_SWEEP_INTERVAL=1h",
		},
	})
	c := s.Client()
	integration.Reset(t, c, integration.Default)
	docs := "/api/documents/" + tree(t, c)[docsOrg].ID

	// The startup cycle: every reclaimer is due at the sweeper's start, and
	// each cycle ends on a pass that drained it.
	s.Await(t, "a drained pass of every reclaimer exported", func() bool {
		passes, _ := r.snapshot()
		return passes["files/drained"] > 0 && passes["outbox/drained"] > 0 && passes["inbox/drained"] > 0
	})
	files := func() int64 {
		passes, _ := r.snapshot()
		return passes["files/drained"] + passes["files/more"] + passes["files/error"]
	}

	t.Run("a recursive delete's nudge is counted under files", func(t *testing.T) {
		before := files()
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "reports"}), http.StatusCreated)
		markBranch(t, c, docs, reports.ID)
		s.Await(t, "the nudged files pass exported", func() bool { return files() > before })
	})

	t.Run("a refused files pass is counted as an error", func(t *testing.T) {
		reports := webtest.Decode[identity](t, c.Post(t, docs+"/directories", map[string]string{"parent_id": "root", "name": "refused"}), http.StatusCreated)
		c.Post(t, docs+"/directories/"+reports.ID+"/files?name=q3.txt", webtest.Raw{ContentType: "text/plain", Body: []byte("q3")}).Expect(t, http.StatusCreated)
		passes, _ := r.snapshot()
		refusals := passes["files/error"]

		f.Sever()
		t.Cleanup(func() { f.Restore(t) })
		markBranch(t, c, docs, reports.ID)
		s.Await(t, "the refused files pass exported", func() bool {
			passes, _ := r.snapshot()
			return passes["files/error"] > refusals
		})
	})

	t.Run("sweep.backlogged reports every reclaimer at rest", func(t *testing.T) {
		want := map[string]int64{"files": 0, "outbox": 0, "inbox": 0}
		s.Await(t, "sweep.backlogged at 0 for every reclaimer", func() bool {
			_, backlogged := r.snapshot()
			return maps.Equal(backlogged, want)
		})
	})
}
