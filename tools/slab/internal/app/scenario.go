package app

import (
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenarios"
)

func defineScenarioNodes(g *graph.Graph, n *Nodes) {
	base, client, grafana := n.Base, n.Client, n.Grafana
	n.Service = g.Define("service", func(s *graph.Scope) (scenarios.Service, error) {
		return scenarios.Service{URL: s.Use(base), Client: s.Use(client)}, nil
	})
	service := n.Service
	n.GrafanaUp = g.Define("grafana up", func(s *graph.Scope) (scenarios.Grafana, error) {
		url := s.Use(grafana)
		return scenarios.Grafana{URL: url, Mimir: httpx.NewMimir(httpx.NewClient(url))}, nil
	}, graph.After(service))
}

func mountScenarios(root *cli.Command, n *Nodes) {
	all := scenarios.All(scenarios.Nodes{
		Service:   n.Service,
		GrafanaUp: n.GrafanaUp,
		Repo:      n.Repo,
		NoColor:   n.NoColor,
	})
	root.Footer = func(w io.Writer) { scenario.WriteListing(w, all...) }
	root.Add(scenario.Commands(all...)...)
}
