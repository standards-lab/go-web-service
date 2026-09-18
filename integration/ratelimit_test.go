//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

// A client over the configured limit gets a 429 problem document with
// Retry-After and a request_id extension; the liveness probe stays
// unaffected regardless.
func TestRateLimit_RequestOverTheLimitIsA429(t *testing.T) {
	s := integration.Start(t, integration.Options{
		Env: []string{"APP_RATE_LIMIT_REQUESTS=3", "APP_RATE_LIMIT_WINDOW=1m"},
	})
	c := s.Client()

	for i := 1; i <= 3; i++ {
		webtest.Decode[organizationPage](t, c.Get(t, organizations), http.StatusOK)
	}

	res := c.Get(t, organizations)
	p := res.Problem(t, http.StatusTooManyRequests)

	if got := res.Header.Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if id, ok := p.Extras["request_id"].(string); !ok || id == "" {
		t.Errorf("429 problem carries no request_id: %+v", p)
	}

	if !webtest.Live(s.URL()) {
		t.Error("liveness probe failed while the client's own limit was exhausted")
	}
}
