package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// mimirQueryRoute is Grafana's datasource proxy to the Mimir datasource
// compose/grafana provisions (uid mimir), which forwards to Mimir's
// Prometheus API prefix, and the instant query under it.
const mimirQueryRoute = "/api/datasources/proxy/uid/mimir/api/v1/query"

// Mimir sends instant queries to Mimir, the observability profile's
// metrics store, through Grafana's datasource proxy, so a reader of
// metrics needs only the Grafana it already probes, and no second base
// URL.
type Mimir struct {
	grafana *Client
}

// NewMimir returns a Mimir over grafana, a client bound to Grafana's base
// URL.
func NewMimir(grafana *Client) *Mimir {
	return &Mimir{grafana: grafana}
}

// Sample is one series of an instant query's vector: its labels, the
// metric name among them as __name__ when the query keeps it, and its
// value at Time. For a selector Time is the evaluation time, not the time
// the sample was written; timestamp() gives the latter as a value.
type Sample struct {
	Labels map[string]string
	Time   time.Time
	Value  float64
}

// Query evaluates promql as an instant query now and returns its vector.
// A query that is not a vector (a scalar, a matrix) is an error, as is any
// status other than 200, whose body is Prometheus's error document.
func (m *Mimir) Query(ctx context.Context, promql string) ([]Sample, error) {
	res, err := m.grafana.Get(ctx, mimirQueryRoute+"?"+url.Values{"query": {promql}}.Encode())
	if err != nil {
		return nil, err
	}
	if err := res.Expect(http.StatusOK); err != nil {
		return nil, fmt.Errorf("query %s: %w", promql, err)
	}
	samples, err := decodeVector(res.Body)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", promql, err)
	}
	return samples, nil
}

// vectorResponse is the Prometheus API's envelope around an instant
// query's result, with each entry's value left as its JSON pair.
type vectorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// decodeVector decodes an instant query's response: a success whose result
// is a vector, each entry's value a pair of the evaluation time in
// fractional Unix seconds and the value as a string.
func decodeVector(body []byte) ([]Sample, error) {
	var r vectorResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode: %w; body: %s", err, body)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("status %q: %s", r.Status, r.Error)
	}
	if r.Data.ResultType != "vector" {
		return nil, fmt.Errorf("result type %q, want vector", r.Data.ResultType)
	}
	samples := make([]Sample, 0, len(r.Data.Result))
	for _, e := range r.Data.Result {
		at, ok := e.Value[0].(float64)
		if !ok {
			return nil, fmt.Errorf("sample time %v is not a number", e.Value[0])
		}
		text, ok := e.Value[1].(string)
		if !ok {
			return nil, fmt.Errorf("sample value %v is not a string", e.Value[1])
		}
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("sample value %q: %w", text, err)
		}
		labels := e.Metric
		if labels == nil {
			labels = map[string]string{}
		}
		samples = append(samples, Sample{Labels: labels, Time: time.UnixMilli(int64(at * 1000)), Value: v})
	}
	return samples, nil
}

// Await queries promql every interval until until reports true of its
// samples, and returns those samples. A failed query ends the wait with
// its error. When limit passes first it returns an error naming the query
// and the limit, which the caller wraps with what it waited for; when ctx
// ends it returns ctx's error.
func (m *Mimir) Await(ctx context.Context, promql string, every, limit time.Duration, until func([]Sample) bool) ([]Sample, error) {
	deadline := time.Now().Add(limit)
	for {
		samples, err := m.Query(ctx, promql)
		if err != nil {
			return nil, err
		}
		if until(samples) {
			return samples, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("%s: not met within %s", promql, limit)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(every, remaining)):
		}
	}
}
