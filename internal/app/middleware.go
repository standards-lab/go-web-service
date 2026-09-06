package app

import (
	"github.com/standards-lab/go-web-sdk"
	mw "github.com/standards-lab/go-web-sdk/middleware"
)

// middleware declares the router-level stack, outermost first. It takes
// infra, not dom: request logging, and cross-cutting concerns like it, need
// infrastructure primitives, not domain services. A middleware that has to
// reach a domain service is domain logic, and belongs in a route or a
// reactor instead.
func middleware(infra *Infrastructure) []web.Middleware {
	return []web.Middleware{
		mw.RequestLogger(infra.Logger),
	}
}
