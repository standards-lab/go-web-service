package app

import (
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenarios"
)

// defineScenarioNodes defines on g into n the need nodes the scenarios
// declare, over the nodes defineInfrastructure defined: service, the
// running service at the base node's URL with the client node's client
// bound to it, and grafana up, the running Grafana at the grafana node's
// URL with a Mimir client bound to it, which the sweeper scenario reads
// metrics through. Each value is a lifecycle Starter that probes what it names, so the
// dispatcher starts it after the Build and a scenario that declares it
// runs no step until it answers; a failed probe is reported
// "startup: <node>: ..." naming the mise task that fixes it.
//
// grafana up is ordered After service. go-core's lifecycle starts a
// System's nodes in layers by longest dependency path and stops at the
// first layer with a failure, and grafana up, reading only the grafana
// setting, would otherwise sit in a lower layer than service, which reads
// base and client: with both down, Grafana's failure would be reported and
// service's never probed. The After edge puts grafana up a layer above
// service whenever both are in the System, so with nothing running the
// failure names service and mise run serve. After builds nothing, so a
// scenario that declares only grafana up would not gain service.
func defineScenarioNodes(g *graph.Graph, n *Nodes) {
	base, client, grafana := n.Base, n.Client, n.Grafana
	n.Service = g.Define("service", func(s *graph.Scope) (scenarios.Service, error) {
		return scenarios.Service{URL: s.Use(base), Client: s.Use(client)}, nil
	})
	service := n.Service
	n.GrafanaUp = g.Define("grafana up", func(s *graph.Scope) (scenarios.Grafana, error) {
		s.After(service)
		url := s.Use(grafana)
		return scenarios.Grafana{URL: url, Mimir: httpx.NewMimir(httpx.NewClient(url))}, nil
	})
}

// mountScenarios mounts go-cli-sdk's scenario parent at root, with one leaf
// per scenario the scenarios package builds over the nodes from n it
// reads, and sets root's Footer to the same listing the parent's help ends
// with, so root help ends with it too. Each leaf declares its own
// scenario's nodes, so a scenario builds and starts only what it reads;
// the parent declares none, and writing the listing builds nothing.
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
