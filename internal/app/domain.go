package app

import (
	"github.com/standards-lab/go-core/lifecycle"
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
// the Infrastructure struct itself, and registers its startup verification
// on lc at the domains' stage, after the schema's.
func newDomain(infra *Infrastructure, lc *lifecycle.Coordinator) *Domain {
	org := organization.New(infra.SQL, infra.Storage)
	org.Register(lc)
	doc := document.New(infra.SQL, infra.Storage, idleSweep{})
	doc.Register(lc)
	return &Domain{Organization: org, Document: doc}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it, each handler handed its policy from cfg at the
// construction site (cfg.Reads.Limits() for a collection read).
func mountAPI(dom *Domain, cfg *config.Config) *web.Group {
	api := web.NewGroup("/api")
	// The organization list continues by cursor: its read model's keyset
	// continuation holds on every sort it accepts.
	orgReads := cfg.Reads.Limits()
	orgReads.Cursor = true
	api.Mount(organization.Routes(dom.Organization, orgReads))
	// The document listings continue by cursor: blobfs's listings issue a
	// cursor on every sort that can continue and page by number otherwise.
	docReads := cfg.Reads.Limits()
	docReads.Cursor = true
	api.Mount(document.Routes(dom.Document, docReads))
	return api
}
