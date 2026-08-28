package organization

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"uuid"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
)

var (
	// ErrValidation classifies a command input rejection; wrapped with the
	// field-level reason, it reaches the wire as a 400's detail.
	ErrValidation = errors.New("invalid command")

	// ErrCycle reports a transfer whose new parent sits inside the
	// organization's own subtree, the organization itself included.
	ErrCycle = errors.New("transfer would create a cycle")
)

// codePattern mirrors the schema's cc_organization_code check, so a bad
// code is a 400 with detail rather than a database check violation.
var codePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Service is the organization domain service: the layer's public API, one
// method per endpoint, every operation delegated whole to the translation
// file. Queries return data; commands validate their input, run guarded,
// and return [Identity] only.
type Service struct {
	db *database.DB
}

// New constructs the service over the database it reads from.
func New(db *database.DB) *Service {
	return &Service{db: db}
}

// List returns one page of organizations and the total count, honoring the
// parsed query's directives and exact-match filters. An unknown sort or
// filter field is a *operation.UnknownFieldError.
func (s *Service) List(
	ctx context.Context,
	q web.Query,
) ([]Organization, int, error) {
	return selectOrganizations(ctx, s.db, q)
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

// Create creates an organization under the stated parent — nil for a root —
// returning its engine-minted identity. A duplicate sibling code or a
// nonexistent parent surfaces as a constraint violation.
func (s *Service) Create(
	ctx context.Context,
	c CreateOrganization,
) (Identity, error) {
	if err := validCode(c.Code); err != nil {
		return Identity{}, err
	}
	if err := validName(c.Name); err != nil {
		return Identity{}, err
	}
	if err := validParent(c.ParentID); err != nil {
		return Identity{}, err
	}
	return insertOrganization(ctx, s.db, c)
}

// Edit rewrites the organization's descriptive fields under the version
// guard, returning the advanced identity.
func (s *Service) Edit(
	ctx context.Context,
	id string,
	version int64,
	e EditOrganization,
) (Identity, error) {
	if err := validCode(e.Code); err != nil {
		return Identity{}, err
	}
	if err := validName(e.Name); err != nil {
		return Identity{}, err
	}
	return editOrganization(ctx, s.db, id, version, e)
}

// Transfer moves the organization under a new parent — nil for the root —
// under the version guard. A destination inside the organization's own
// subtree is [ErrCycle].
func (s *Service) Transfer(
	ctx context.Context,
	id string,
	version int64,
	t TransferOrganization,
) (Identity, error) {
	if err := validParent(t.ParentID); err != nil {
		return Identity{}, err
	}
	return transferOrganization(ctx, s.db, id, version, t)
}

// Delete removes the organization under the version guard; children block
// deletion.
func (s *Service) Delete(
	ctx context.Context,
	id string,
	version int64,
) error {
	return deleteOrganization(ctx, s.db, id, version)
}

// validCode rejects a code the schema's pattern would refuse.
func validCode(code string) error {
	if !codePattern.MatchString(code) {
		return fmt.Errorf("%w: code must be lowercase words joined by single hyphens", ErrValidation)
	}
	return nil
}

// validName rejects an empty name.
func validName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrValidation)
	}
	return nil
}

// validParent rejects a parent id that is not a UUID; nil is the root.
func validParent(parent *string) error {
	if parent == nil {
		return nil
	}
	if _, err := uuid.Parse(*parent); err != nil {
		return fmt.Errorf("%w: parent_id must be a UUID", ErrValidation)
	}
	return nil
}
