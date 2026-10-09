package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/domain/organization"
)

// defineDomain defines the domain services on g into n, one node per domain
// layer. Domain services are defined in the module's base-layer domain
// packages, one package per layer under domain/, and each node's
// constructor builds its service from the infrastructure nodes it Uses,
// never from Nodes itself. A domain service holds no resource and runs
// nothing, so none is a lifecycle participant; the seeder its seed
// contributions join checks its statements (admin.go). It constructs
// nothing.
func defineDomain(g *graph.Graph, n *Nodes) {
	n.Organization = g.Define("organization", func(s *graph.Scope) (*organization.Service, error) {
		return organization.New(s.Use(n.SQL), s.Use(n.Files), s.Use(n.Logger)), nil
	})
	// The wake node is the document layer's Sweeper, which it nudges after
	// each branch it marks.
	n.Document = g.Define("document", func(s *graph.Scope) (*document.Service, error) {
		return document.New(s.Use(n.SQL), s.Use(n.Files), s.Use(n.Wake)), nil
	})
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it, each handed the paging policy and the server's transfer
// sizing from the config node and the logger node for its error writer.
// Every collection read may continue by cursor. Each read issues a cursor on
// every sort its keyset can continue and pages by number otherwise, as the
// organization list does on its one nullable field, parent_id.
func mountAPI(s *graph.Scope, n *Nodes) *web.Group {
	cfg, logger := s.Use(n.Config), s.Use(n.Logger)
	api := web.NewGroup("/api")
	reads := cfg.Reads.Limits()
	reads.Cursor = true
	api.Mount(organization.Routes(s.Use(n.Organization), reads, cfg.Server.Transfer, logger))
	api.Mount(document.Routes(s.Use(n.Document), reads, cfg.Server.Transfer, logger))
	return api
}
