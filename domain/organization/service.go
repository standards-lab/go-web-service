package organization

import (
	"context"
	"net/url"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
)

// Service is the organization domain service: the layer's public API, one
// method per endpoint, every operation delegated whole to the translation
// file. Queries only, until the writes slice arrives.
type Service struct {
	db *database.DB
}

// New constructs the service over the database it reads from.
func New(db *database.DB) *Service {
	return &Service{db: db}
}

// List returns one page of organizations and the total count, honoring the
// parsed directives and the exact-match filters. An unknown sort or filter
// field is a *query.UnknownFieldError.
func (s *Service) List(
	ctx context.Context,
	d web.Directives,
	filters url.Values,
) ([]Organization, int, error) {
	return selectOrganizations(ctx, s.db, d, filters)
}

// Find returns the organization with the given id, or [sql.ErrNoRows].
func (s *Service) Find(
	ctx context.Context,
	id string,
) (Organization, error) {
	return selectOrganization(ctx, s.db, "id", id)
}

// FindByPath resolves a composed organization path ("/acme/engineering") to
// its node, or [sql.ErrNoRows].
func (s *Service) FindByPath(
	ctx context.Context,
	path string,
) (Organization, error) {
	return selectOrganization(ctx, s.db, "path", path)
}
