package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/metrics"
)

// grafana serves canned answers for the Mimir datasource proxy's instant
// query: answer returns the status and body for the nth query (from 0),
// and every query's PromQL is recorded into got.
func grafana(t *testing.T, got *[]string, answer func(n int) (int, string)) *metrics.Client {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/datasources/proxy/uid/mimir/api/v1/query" {
			t.Errorf("path = %q, want the Mimir proxy's query", r.URL.Path)
		}
		*got = append(*got, r.URL.Query().Get("query"))
		status, body := answer(int(n.Add(1) - 1))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return metrics.NewClient(httpx.NewClient(srv.URL))
}

const vector = `{"status":"success","data":{"resultType":"vector","result":[
	{"metric":{"sweep_reclaimer":"files","sweep_outcome":"drained"},"value":[1792106085.933,"13"]},
	{"metric":{},"value":[1792106085.5,"0.5"]}]}}`

// Query sends the PromQL through the proxy and decodes the vector: each
// series' labels, its value, and its evaluation time to the millisecond.
func TestQuery_DecodesTheVector(t *testing.T) {
	var got []string
	c := grafana(t, &got, func(int) (int, string) { return http.StatusOK, vector })

	samples, err := c.Query(context.Background(), `sum by (x) (y{job="a b"})`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `sum by (x) (y{job="a b"})` {
		t.Errorf("queries sent = %q, want the PromQL as given", got)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %+v, want 2", samples)
	}
	s := samples[0]
	if s.Labels["sweep_reclaimer"] != "files" || s.Labels["sweep_outcome"] != "drained" || s.Value != 13 {
		t.Errorf("first sample = %+v", s)
	}
	if want := time.UnixMilli(1792106085933); !s.Time.Equal(want) {
		t.Errorf("time = %v, want %v", s.Time, want)
	}
	if samples[1].Labels == nil || samples[1].Value != 0.5 {
		t.Errorf("second sample = %+v, want empty labels and 0.5", samples[1])
	}
}

// Every answer that is not a vector of numbers is an error naming the
// query and why.
func TestQuery_RefusesWhatIsNotAVector(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"a bad query":      {http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"parse error"}`, "status = 400"},
		"an error status":  {http.StatusOK, `{"status":"error","error":"boom"}`, `status "error": boom`},
		"a scalar":         {http.StatusOK, `{"status":"success","data":{"resultType":"scalar","result":[]}}`, `result type "scalar", want vector`},
		"a non-number":     {http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"x"]}]}}`, `sample value "x"`},
		"not JSON at all":  {http.StatusOK, `<html>`, "decode"},
		"a value unquoted": {http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,2]}]}}`, "is not a string"},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string
			c := grafana(t, &got, func(int) (int, string) { return tc.status, tc.body })
			_, err := c.Query(context.Background(), "up")
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "query up: ") {
				t.Errorf("err = %v, want it to name the query and %q", err, tc.want)
			}
		})
	}
}

// Await queries until the condition holds and returns those samples.
func TestAwait_ReturnsOnceTheConditionHolds(t *testing.T) {
	var got []string
	c := grafana(t, &got, func(n int) (int, string) {
		if n < 2 {
			return http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[]}}`
		}
		return http.StatusOK, vector
	})
	samples, err := c.Await(context.Background(), "up", time.Millisecond, time.Minute, func(s []metrics.Sample) bool { return len(s) > 0 })
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || len(got) != 3 {
		t.Errorf("samples = %d after %d queries, want 2 after 3", len(samples), len(got))
	}
}

// A condition that never holds ends the wait at its limit, with an error
// wrapping ErrNotYet that names the query and the limit.
func TestAwait_GivesUpAtTheLimit(t *testing.T) {
	var got []string
	c := grafana(t, &got, func(int) (int, string) { return http.StatusOK, vector })
	_, err := c.Await(context.Background(), "up", time.Millisecond, 20*time.Millisecond, func([]metrics.Sample) bool { return false })
	if !errors.Is(err, metrics.ErrNotYet) {
		t.Fatalf("err = %v, want ErrNotYet", err)
	}
	if want := "up: not met within 20ms"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// A failed query ends the wait with its error, without waiting out the
// limit.
func TestAwait_EndsOnAFailedQuery(t *testing.T) {
	var got []string
	c := grafana(t, &got, func(int) (int, string) { return http.StatusBadGateway, `bad gateway` })
	_, err := c.Await(context.Background(), "up", time.Hour, time.Hour, func([]metrics.Sample) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "status = 502") || len(got) != 1 {
		t.Errorf("err = %v after %d queries, want the 502 after one", err, len(got))
	}
}
