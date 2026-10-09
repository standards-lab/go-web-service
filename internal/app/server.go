package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
)

// defineServer defines the request edge on g into n: the readiness the
// probes report, the router, and the server. It constructs nothing.
func defineServer(g *graph.Graph, n *Nodes) {
	n.Readiness = g.Define("readiness", func(*graph.Scope) (*lifecycle.Readiness, error) {
		return new(lifecycle.Readiness), nil
	})
	n.Router = g.Define("router", newRouter(n))
	n.Server = g.Define("server", newServer(n))
}

// newRouter assembles the router: the middleware stack, the mounts, and the
// probes. The probes register on the router's native mux, outside every
// module's middleware, and report the readiness node's value: lifecycle.New
// binds it to the Coordinator after the Build, and the probes read it live
// on every request.
func newRouter(n *Nodes) func(*graph.Scope) (*web.Router, error) {
	return func(s *graph.Scope) (*web.Router, error) {
		router := web.NewRouter()
		router.Use(middleware(s, n)...)
		for _, m := range routes(s, n) {
			router.Mount(m)
		}

		// The zero Problem keeps the SDK's readiness defaults: type
		// about:blank, status 503 with its status text as the title, the
		// generic detail, and the checks extension member. The service
		// names no problem type of its own yet.
		web.RegisterHealth(router, s.Use(n.Readiness), web.Problem{})
		return router, nil
	}
}

// newServer constructs the server over the router from the config node's
// server block. Its value is a lifecycle.Subsystem and a
// lifecycle.Monitored, so the Coordinator starts it, drains it, and watches
// its Err channel with no registration here. It orders itself after every
// reactor and after the schema, so it is the top layer: it starts after
// everything else and drains first, and in-flight requests complete before
// what they run on closes.
func newServer(n *Nodes) func(*graph.Scope) (*web.Server, error) {
	return func(s *graph.Scope) (*web.Server, error) {
		for _, r := range n.Reactors {
			s.After(r)
		}
		s.After(n.Schema)
		return web.NewServer(s.Use(n.Config).Server, s.Use(n.Router), s.Use(n.Logger)), nil
	}
}
