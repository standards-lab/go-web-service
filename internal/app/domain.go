package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/domain/organization"
)

func defineDomain(g *graph.Graph, n *Nodes) {
	n.Organization = g.Define("organization", n.newOrganization)
	n.Document = g.Define("document", n.newDocument)
}

func (n *Nodes) newOrganization(s *graph.Scope) (*organization.Service, error) {
	return organization.New(s.Use(n.SQL), s.Use(n.Files), s.Use(n.Logger), s.Use(n.Runtime).Recorder), nil
}

func (n *Nodes) newDocument(s *graph.Scope) (*document.Service, error) {
	return document.New(s.Use(n.SQL), s.Use(n.Files), s.Use(n.Nudge), s.Use(n.Runtime).Recorder), nil
}

func mountAPI(s *graph.Scope, n *Nodes) *web.Group {
	cfg, logger := s.Use(n.Config), s.Use(n.Logger)
	api := web.NewGroup("/api")
	reads := cfg.Reads.Limits()
	reads.Cursor = true
	api.Mount(organization.Routes(s.Use(n.Organization), reads, cfg.Server.Transfer, logger))
	api.Mount(document.Routes(s.Use(n.Document), reads, cfg.Server.Transfer, logger))
	return api
}
