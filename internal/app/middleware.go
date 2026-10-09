package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-web-sdk"
	mw "github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/middleware/rate-limit"
)

// middleware declares the router-level stack, outermost first. It reads
// infrastructure nodes only, the config and the logger: request logging,
// and cross-cutting concerns like it, need infrastructure primitives, not
// domain services. A middleware that has to reach a domain service is
// domain logic, and belongs in a route or a reactor instead.
//
// Tracing runs outermost so every later middleware, and the handler itself,
// sees the request inside its span. It records to the providers the
// telemetry node installs, so the stack Uses that node: the use is what
// brings telemetry into the Build, since the connections' edges to it only
// order them after it and never build it. RequestID sits next, sourcing the trace
// id observability.RequestIDSource reads off that span, so the id
// RequestLogger records, the X-Request-Id response header, and any problem
// document's request_id extension all carry the same value. Reordering any
// of the three breaks that chain: tracing after RequestID leaves no span for
// the source function to read, and RequestLogger before RequestID logs
// before the id exists.
//
// Recoverer follows RequestLogger, so a handler's panic is answered with a
// 500 problem carrying the request's id and logged as that status; the SDK
// allows either order between the two.
//
// RateLimit sits innermost, inside RequestID and RequestLogger, so a
// rejected request still carries a correlation id and is logged with its
// 429 status. It is wrapped in Maybe against mw.NotProbe so the liveness
// and readiness probes are never rate-limited.
func middleware(s *graph.Scope, n *Nodes) []web.Middleware {
	cfg, logger := s.Use(n.Config), s.Use(n.Logger)
	s.Use(n.Telemetry)
	return []web.Middleware{
		observability.NewMiddleware(observabilityConfig(cfg)),
		mw.RequestID(mw.WithIDSource(observability.RequestIDSource)),
		mw.RequestLogger(logger),
		mw.Recoverer(logger),
		mw.Maybe(ratelimit.New(cfg.RateLimit), mw.NotProbe),
	}
}
