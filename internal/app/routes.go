package app

import (
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/domain"
)

// routes composes the API module: the one /api group, with each domain
// layer's route group mounted into it and each handler handed its policy —
// the reads limits today — at the construction site.
func routes(dom *domain.Domain, cfg *config.Config) []*web.Module {
	api := web.NewGroup("/api")
	api.Mount(organization.Routes(dom.Organization, cfg.Reads.Limits()))
	return []*web.Module{web.NewModule(api)}
}
