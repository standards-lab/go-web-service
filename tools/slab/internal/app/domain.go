package app

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// domainCommands builds the domain layer's commands on the tree, one per
// domain package moved onto go-cli-sdk, each handed the client node from n
// and the output source to render through. They mount directly on the
// root: slab has no container command for the domains the way the service
// has an /api group.
func domainCommands(n *Nodes) []*cli.Command {
	return []*cli.Command{
		organization.Commands(n.Client, outputSource(n)),
	}
}

// Domain composes the domain clients of the packages still on cobra, one
// field per domain package. Each field is the constructor the package's
// commands call when a subcommand runs, not the client itself: the client
// binds to the base URL, which is parsed after the tree is built.
type Domain struct {
	Document func() *document.Client
}

// newDomain wires the cobra domain layer over infra: each domain package's
// client constructor is closed here over the infrastructure's client, which
// is itself constructed on demand.
func newDomain(infra *Infrastructure) *Domain {
	return &Domain{
		Document: func() *document.Client {
			return document.NewClient(infra.Client())
		},
	}
}

// mountDomain builds the cobra domain commands, one per domain package
// still on cobra, each handed its client constructor from dom and the
// output to render through.
func mountDomain(dom *Domain, out *output.Output) []*cobra.Command {
	return []*cobra.Command{
		document.Commands(dom.Document, out),
	}
}
