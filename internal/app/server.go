package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
)

func defineServer(g *graph.Graph, n *Nodes) {
	n.Readiness = g.Fresh[lifecycle.Readiness]("readiness")
	n.Router = g.Define("router", n.newRouter)
	n.Server = g.Define("server", n.newServer, graph.After(n.Reactors, n.Schema))
}

func (n *Nodes) newRouter(s *graph.Scope) (*web.Router, error) {
	router := web.NewRouter()
	router.Use(middleware(s, n)...)
	for _, m := range routes(s, n) {
		router.Mount(m)
	}

	web.RegisterHealth(router, s.Use(n.Readiness), web.Problem{})
	return router, nil
}

func (n *Nodes) newServer(s *graph.Scope) (*web.Server, error) {
	return web.NewServer(s.Use(n.Config).Server, s.Use(n.Router), s.Use(n.Logger)), nil
}
