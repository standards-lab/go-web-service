package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

func mountDomain(root *cli.Command, n *Nodes) {
	out := outputSource(n)
	root.Add(organization.Commands(n.Client, out)...)
	root.Add(document.Commands(n.Client, out)...)
}
