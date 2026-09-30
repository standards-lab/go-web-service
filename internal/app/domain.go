package app

import (
	"log/slog"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/internal/config"
)

// Domain composes the application's domain services, one field per domain
// layer. Domain services are defined in the module's base-layer domain
// packages, one package per layer under domain/, and constructed here from
// the infrastructure fields they depend on.
type Domain struct {
	Organization *organization.Service
	Document     *document.Service
}

// newDomain wires the domain layer over infra: each domain package's
// service is constructed here from the infrastructure fields it uses, never
// the Infrastructure struct itself. A domain service holds no resource and
// runs nothing, so it knows no lifecycle; the seeder its seed
// contributions join checks its statements (admin.go). sweep is the
// sweep's wake source, the document layer's Sweeper, which it nudges after
// each branch it marks.
func newDomain(infra *Infrastructure, sweep document.Sweeper) *Domain {
	return &Domain{
		Organization: organization.New(infra.SQL, infra.Storage, infra.Logger),
		Document:     document.New(infra.SQL, infra.Storage, sweep),
	}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it, each handed the paging policy and the server's transfer
// sizing from cfg and the service's logger for its error writer. Every
// collection read may continue by cursor. Each read issues a cursor on
// every sort its keyset can continue and pages by number otherwise, as the
// organization list does on its one nullable field, parent_id.
func mountAPI(dom *Domain, cfg *config.Config, logger *slog.Logger) *web.Group {
	api := web.NewGroup("/api")
	reads := cfg.Reads.Limits()
	reads.Cursor = true
	api.Mount(organization.Routes(dom.Organization, reads, cfg.Server.Transfer, logger))
	api.Mount(document.Routes(dom.Document, reads, cfg.Server.Transfer, logger))
	return api
}
