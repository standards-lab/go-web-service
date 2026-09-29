package app

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"
	"log/slog"

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
// runs nothing, so it knows no lifecycle; what it has for startup is its
// Verify, which the root declares on lc at stageVerify, once the schema is
// corrected. sweep is the sweep's wake source, the document layer's
// Sweeper, which it nudges after each branch it marks.
func newDomain(infra *Infrastructure, sweep document.Sweeper, lc *lifecycle.Coordinator) *Domain {
	org := organization.New(infra.SQL, infra.Storage)
	lc.Add(lifecycle.Service{Name: "organization", Stage: stageVerify, Start: org.Verify})
	doc := document.New(infra.SQL, infra.Storage, sweep)
	lc.Add(lifecycle.Service{Name: "document", Stage: stageVerify, Start: doc.Verify})
	return &Domain{Organization: org, Document: doc}
}

// mountAPI builds the API mount, /api, with each domain layer's route group
// mounted into it, each handler handed its policy from cfg at the
// construction site (cfg.Reads.Limits() for a collection read) and the
// service's logger for its error writer.
func mountAPI(dom *Domain, cfg *config.Config, logger *slog.Logger) *web.Group {
	api := web.NewGroup("/api")
	// The organization list continues by cursor: its read model's keyset
	// continuation holds on every sort it accepts.
	orgReads := cfg.Reads.Limits()
	orgReads.Cursor = true
	api.Mount(organization.Routes(dom.Organization, orgReads, logger))
	// The document listings continue by cursor: blobfs's listings issue a
	// cursor on every sort that can continue and page by number otherwise.
	docReads := cfg.Reads.Limits()
	docReads.Cursor = true
	api.Mount(document.Routes(dom.Document, docReads, logger))
	return api
}
