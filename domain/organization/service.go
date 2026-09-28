package organization

import (
	"context"
	"errors"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
)

// ErrCycle reports a transfer whose new parent sits inside the
// organization's own subtree, the organization itself included.
var ErrCycle = errors.New("transfer would create a cycle")

// Service is the organization domain service: the layer's public API, one
// method per endpoint, every operation delegated whole to the store.
// Queries return data; commands validate their input, each command type
// owning its rules, run guarded, and return Identity only.
type Service struct {
	store *store
}

// New constructs the service over the database and the object storage the
// domains share. Construction compiles and binds the statements and
// performs no I/O.
func New(db *data.Database, st *data.Storage) *Service {
	return &Service{store: newStore(db, st)}
}

// Verify prepares every statement and the read contract against the
// migrated schema; the composition root runs it at startup, once the
// schema is corrected.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// List returns one page of organizations and the read's paging, honoring
// the parsed query's page or cursor, sort, and filters. A cursor that did
// not come from this read is the request's error. An unknown sort or filter field,
// an operator the read model does not support, or a value the engine
// cannot read unwraps to query.ErrDirectives.
func (s *Service) List(ctx context.Context, q web.Query) ([]Organization, web.Paging, error) {
	return s.store.list(ctx, q)
}

// Find returns the organization with the given id, or sql.ErrNoRows.
func (s *Service) Find(ctx context.Context, id string) (Organization, error) {
	return s.store.find(ctx, "id", id)
}

// FindByPath resolves a composed organization path ("/acme/engineering") to
// its node, or sql.ErrNoRows.
func (s *Service) FindByPath(ctx context.Context, path string) (Organization, error) {
	return s.store.find(ctx, "path", path)
}

// Create creates an organization under the stated parent, nil for a root,
// returning its engine-minted identity. A duplicate sibling code or a
// nonexistent parent surfaces as a constraint violation.
func (s *Service) Create(ctx context.Context, c CreateOrganization) (Identity, error) {
	if err := c.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.create(ctx, c)
}

// Edit replaces the organization's descriptive fields under the version
// guard, returning the advanced identity.
func (s *Service) Edit(ctx context.Context, id string, version int64, e EditOrganization) (Identity, error) {
	if err := e.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.edit(ctx, id, version, e)
}

// Transfer is an action: it moves the organization under a new parent, nil
// for the root, under the version guard and the tree lock. A destination
// inside the organization's own subtree is ErrCycle.
func (s *Service) Transfer(ctx context.Context, id string, version int64, t TransferOrganization) (Identity, error) {
	if err := t.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.transfer(ctx, id, version, t)
}

// Delete removes the organization under the version guard; children block
// deletion through the foreign key.
func (s *Service) Delete(ctx context.Context, id string, version int64) error {
	return s.store.delete(ctx, id, version)
}

// PutLogo stores the upload as the organization's logo and makes it the
// active one, retiring the logo it replaces, and returns the new file's
// identity. A media type outside the logo's allowlist is refused before any
// I/O; a nonexistent organization is sql.ErrNoRows, and a concurrent
// replacement that activated first is a unique violation.
func (s *Service) PutLogo(ctx context.Context, id string, u web.Upload) (Identity, error) {
	ext, err := logoExtension(u.MediaType)
	if err != nil {
		return Identity{}, err
	}
	return s.store.putLogo(ctx, id, u, ext)
}

// Logo returns the organization's active logo, or the missing row when it
// has none.
func (s *Service) Logo(ctx context.Context, id string) (Logo, error) {
	return s.store.logo(ctx, id)
}

// DeleteLogo retires the organization's active logo, its row and its
// object, or returns sql.ErrNoRows when it has none.
func (s *Service) DeleteLogo(ctx context.Context, id string) error {
	return s.store.deleteLogo(ctx, id)
}
