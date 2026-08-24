package app

import (
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/internal/domain"
)

// routes returns nothing in the template; an application built from it
// mounts its domain-service modules here, over dom.
func routes(dom *domain.Domain) []*web.Module {
	return nil
}
