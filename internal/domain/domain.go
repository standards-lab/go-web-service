package domain

import (
	"github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/internal/infrastructure"
)

// Domain composes the application's domain services, one field per domain
// layer.
type Domain struct {
	Organization *organization.Service
}

// New wires the domain layer over infra: each domain package's service is
// constructed here from the infrastructure fields it uses, never the
// Infrastructure struct itself.
func New(infra *infrastructure.Infrastructure) *Domain {
	return &Domain{
		Organization: organization.New(infra.DB),
	}
}
