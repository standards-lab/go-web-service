package document

import (
	"context"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
)

// Stage is the lifecycle stage the domains verify their statements in:
// after the schema (1), before the root.
const Stage = 2

// Service is the document domain service: the layer's public API, one
// method per endpoint, every operation delegated whole to the store. Every
// method takes the organization's id first and checks the scope of every
// id it is given against that organization's document root; an id where
// a directory is taken may be RootAlias. Queries return data; commands
// validate their input before any I/O and return Identity only.
type Service struct {
	store *store
}

// New constructs the service over the database and the object storage the
// domains share. Construction compiles and binds the statements and
// performs no I/O.
func New(db *data.Database, st *data.Storage) *Service {
	return &Service{store: newStore(db, st)}
}

// Register declares the domain's startup verification on lc.
func (s *Service) Register(lc *lifecycle.Coordinator) {
	lc.Add(lifecycle.Service{Name: "document", Stage: Stage, Start: s.Verify})
}

// Verify prepares every statement against the migrated schema; startup
// runs it at the domains' stage.
func (s *Service) Verify(ctx context.Context) error { return s.store.Verify(ctx) }

// CreateDirectory creates a directory under the command's parent,
// ensuring the organization's root when the parent is its alias, and
// returns its identity. A nonexistent organization is sql.ErrNoRows, a
// parent outside the root is not found, and a taken name is a conflict.
func (s *Service) CreateDirectory(ctx context.Context, organizationID string, c CreateDirectory) (Identity, error) {
	if err := c.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.createDirectory(ctx, organizationID, c)
}

// Directory returns the directory with its path from the root.
func (s *Service) Directory(ctx context.Context, organizationID, id string) (Directory, error) {
	return s.store.directory(ctx, organizationID, id)
}

// ListDirectories returns one page of the directory's child directories
// and the read's paging, honoring the parsed query's page or cursor, sort,
// and filters. An unknown field, operator, or value, or a cursor that did
// not come from this read, is the request's error.
func (s *Service) ListDirectories(ctx context.Context, organizationID, id string, q web.Query) ([]Directory, web.Paging, error) {
	return s.store.listDirectories(ctx, organizationID, id, q)
}

// ListFiles returns one page of the directory's files, whatever their
// status, as ListDirectories reads its directories.
func (s *Service) ListFiles(ctx context.Context, organizationID, id string, q web.Query) ([]File, web.Paging, error) {
	return s.store.listFiles(ctx, organizationID, id, q)
}

// DeleteDirectory removes the directory. Without recursive a directory
// with contents is blobfs.ErrNotEmpty; with it, the contents are removed
// first, children before their parent, up to the walk's bound. Removing
// the root removes its owner row with it.
func (s *Service) DeleteDirectory(ctx context.Context, organizationID, id string, recursive bool) error {
	return s.store.deleteDirectory(ctx, organizationID, id, recursive)
}

// MoveDirectory is an action: it moves the directory under a new parent
// within the same root, as the command's name, under the version guard.
// The root is refused before any I/O, and a destination inside the
// directory's own subtree is blobfs.ErrCycle.
func (s *Service) MoveDirectory(ctx context.Context, organizationID, id string, version int64, m MoveDirectory) (Identity, error) {
	if id == RootAlias {
		return Identity{}, errRootMove
	}
	if err := m.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.moveDirectory(ctx, organizationID, id, version, m)
}

// PutFile stores the upload as a new file named name in the directory,
// ensuring the root when the directory is its alias, and returns the
// file's identity. A name blobfs would refuse is refused before any I/O; a
// taken name is a conflict.
func (s *Service) PutFile(ctx context.Context, organizationID, directoryID, name string, u web.Upload) (Identity, error) {
	if err := validName(name); err != nil {
		return Identity{}, err
	}
	return s.store.putFile(ctx, organizationID, directoryID, name, u)
}

// File returns the file's metadata.
func (s *Service) File(ctx context.Context, organizationID, id string) (File, error) {
	return s.store.file(ctx, organizationID, id)
}

// Content returns an available file for its download; a file whose write
// has not completed, or whose delete has begun, is not found.
func (s *Service) Content(ctx context.Context, organizationID, id string) (Content, error) {
	return s.store.content(ctx, organizationID, id)
}

// DeleteFile removes the file, its row and its object.
func (s *Service) DeleteFile(ctx context.Context, organizationID, id string) error {
	return s.store.deleteFile(ctx, organizationID, id)
}

// MoveFile is an action: it moves the file into a directory within the
// same root, as the command's name, under the version guard.
func (s *Service) MoveFile(ctx context.Context, organizationID, id string, version int64, m MoveFile) (Identity, error) {
	if err := m.Validate(); err != nil {
		return Identity{}, err
	}
	return s.store.moveFile(ctx, organizationID, id, version, m)
}
