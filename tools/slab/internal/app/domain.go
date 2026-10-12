package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

// mountDomain mounts each domain package's commands at root, one
// root.Add(pkg.Commands(...)...) per domain, each handed the client node
// from n and the output source to render through, over the no color node. The domains define no
// nodes of their own: each command family declares the infrastructure
// nodes it reads. They mount directly on the root: slab has no container
// command for the domains the way the service has an /api group.
func mountDomain(root *cli.Command, n *Nodes) {
	out := outputSource(n)
	root.Add(organization.Commands(n.Client, out)...)
	root.Add(document.Commands(n.Client, out)...)
}
