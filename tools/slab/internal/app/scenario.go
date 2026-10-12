package app

import (
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"

	"github.com/standards-lab/go-web-service/tools/slab/scenarios"
)

// mountScenarios mounts go-cli-sdk's scenario parent at root, with one leaf
// per scenario the scenarios package builds over the nodes from n it
// reads. Each leaf declares its own scenario's nodes, so a scenario builds
// only what it reads; the parent declares none, and its help ends with the
// listing. The root's own Footer still lists the demo scenarios while the
// demo command stands beside this one.
func mountScenarios(root *cli.Command, n *Nodes) {
	root.Add(scenario.Commands(scenarios.All(scenarios.Nodes{Repo: n.Repo, NoColor: n.NoColor})...)...)
}
