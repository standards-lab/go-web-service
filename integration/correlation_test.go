//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/integration"
)

// record is the slice of a JSON log line the correlation check reads: the
// request id RequestLogger records and the trace id the correlating handler
// appends from the span.
type record struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
}

// The id a problem document carries as request_id is the trace id of the
// request's span: the tracing middleware mints it, RequestID adopts it
// through the source function, the SDK surfaces it on the document, and the
// correlating log handler reads the same span independently. One log record
// whose request_id and trace_id both equal the document's id proves every
// link of that chain at once; two presence checks would not.
func TestCorrelation_ProblemAndLogShareTraceID(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	id, ok := p.Extras["request_id"].(string)
	if !ok || id == "" {
		t.Fatalf("403 problem carries no request_id: %+v", p)
	}

	// Await re-scans the whole captured output on each poll, skipping lines
	// that are not JSON records: the OpenTelemetry error handler's own
	// output when no collector answers, for one. A timeout fails with the
	// output, so a missing or mismatched line is visible in the failure.
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
