package app

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// Domain composes the domain clients, one field per domain package. Each
// field is the constructor the package's commands call when a subcommand
// runs, not the client itself: the client binds to the base URL, which is
// parsed after the tree is built.
type Domain struct {
	Organization func() *organization.Client
}

// newDomain wires the domain layer over infra: each domain package's client
// constructor is closed here over the infrastructure's client, which is
// itself constructed on demand.
func newDomain(infra *Infrastructure) *Domain {
	return &Domain{
		Organization: func() *organization.Client {
			return organization.NewClient(infra.Client())
		},
	}
}

// mountDomain builds the domain layer's commands, one per domain package,
// each handed its client constructor from dom and the output to render
// through. They mount directly on the root: slab has no container command
// for the domains the way the service has an /api group.
func mountDomain(dom *Domain, out *output.Output) []*cobra.Command {
	return []*cobra.Command{
		organization.Commands(dom.Organization, out),
	}
}
