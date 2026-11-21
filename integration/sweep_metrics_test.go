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

type receiver struct {
	colmetricpb.UnimplementedMetricsServiceServer

	mu                       sync.Mutex
	passesByReclaimerOutcome map[string]int64
	backloggedByReclaimer    map[string]int64
}

func startReceiver(t *testing.T) (*receiver, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r := &receiver{passesByReclaimerOutcome: map[string]int64{}, backloggedByReclaimer: map[string]int64{}}
	srv := grpc.NewServer()
	colmetricpb.RegisterMetricsServiceServer(srv, r)
	coltracepb.RegisterTraceServiceServer(srv, traceSink{})
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return r, l.Addr().String()
}

func (r *receiver) Export(_ context.Context, req *colmetricpb.ExportMetricsServiceRequest) (*colmetricpb.ExportMetricsServiceResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				switch m.GetName() {
				case "sweep.passes":
					for _, p := range m.GetSum().GetDataPoints() {
						r.passesByReclaimerOutcome[attr(p.GetAttributes(), "sweep.reclaimer")+"/"+attr(p.GetAttributes(), "sweep.outcome")] = p.GetAsInt()
					}
				case "sweep.backlogged":
					for _, p := range m.GetGauge().GetDataPoints() {
						r.backloggedByReclaimer[attr(p.GetAttributes(), "sweep.reclaimer")] = p.GetAsInt()
					}
				}
			}
		}
	}
	return &colmetricpb.ExportMetricsServiceResponse{}, nil
}

type traceSink struct {
	coltracepb.UnimplementedTraceServiceServer
}

func (traceSink) Export(context.Context, *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func attr(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func (r *receiver) snapshot() (passes, backloggedByReclaimer map[string]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.passesByReclaimerOutcome), maps.Clone(r.backloggedByReclaimer)
}

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
