package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

// domainCommands builds the domain layer's commands on the tree, one per
// domain package, each handed the client node from n and the output source
// to render through. They mount directly on the root: slab has no
// container command for the domains the way the service has an /api group.
func domainCommands(n *Nodes) []*cli.Command {
	out := outputSource(n)
	return []*cli.Command{
		organization.Commands(n.Client, out),
		document.Commands(n.Client, out),
	}
}
