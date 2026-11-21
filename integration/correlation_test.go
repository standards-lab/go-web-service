//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

type record struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
}

func TestCorrelation_ProblemAndLogShareTraceID(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	id, ok := p.Extras["request_id"].(string)
	if !ok || id == "" {
		t.Fatalf("403 problem carries no request_id: %+v", p)
	}

	var matched record
	s.Await(t, "correlated log record", func() bool {
		for line := range strings.Lines(s.Output()) {
			var r record
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			if r.RequestID == id {
				matched = r
				return true
			}
		}
		return false
	})
	if matched.TraceID != id {
		t.Errorf("log record for request %s carries trace_id %q, want the same id", id, matched.TraceID)
	}
}
