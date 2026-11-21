package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-web-sdk"
	mw "github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/middleware/rate-limit"
)

func middleware(s *graph.Scope, n *Nodes) []web.Middleware {
	cfg, logger := s.Use(n.Config), s.Use(n.Logger)
	s.Use(n.Telemetry)
	return []web.Middleware{
		observability.NewMiddleware(observabilityConfig(cfg)),
		mw.RequestID(mw.RequestIDOptions{Source: observability.RequestIDSource}),
		mw.RequestLogger(logger),
		mw.Recoverer(logger),
		mw.Maybe(ratelimit.New(cfg.RateLimit), mw.NotProbe),
	}
}
