//go:build integration

package integration_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/standards-lab/go-web-service/integration"
)

func TestRateLimit_RequestOverTheLimitIsA429ProbesUnaffected(t *testing.T) {
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

	for _, probe := range []string{"/healthz", "/readyz"} {
		c.Get(t, probe).Expect(t, http.StatusOK)
	}
}
