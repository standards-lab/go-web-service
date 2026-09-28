package document

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/standards-lab/blobfs"
	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

//go:embed statements/*.sql
var files embed.FS

// walkPage is the size of each page a recursive delete reads: the walk
// reads a directory's first page again after removing it, so it never
// holds a cursor over rows it is removing.
const walkPage = 100

// store is the domain's SQL client: the owner row's statements bound once
// to their typed handles, and the protocols of storage.go as its other
// methods. It is the package's sole importer of the query library, so the
// lowering of a request's query onto blobfs's listings lives here too. The
// owner-row statements are steps the protocols sequence around blobfs's,
// so their methods take the session the protocol hands them.
type store struct {
	db               *data.Database
	storage          *data.Storage
	stmts            *query.Statements
	documentRootRows query.Rows[string]
	organizationRows query.Rows[string]
	bindRoot         query.Statement
	unbindRoot       query.Statement
}

// newStore compiles the statements against the service's catalog, registers
// the inventory under the domain's name, and binds the handles. A compile
// failure is a wiring defect and panics; no I/O happens here.
func newStore(db *data.Database, st *data.Storage) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	db.Register("document", stmts)
	return &store{
		db:               db,
		storage:          st,
		stmts:            stmts,
		documentRootRows: stmts.Statement("document_root").Scan(query.Scalar[string]),
		organizationRows: stmts.Statement("find_organization").Scan(query.Scalar[string]),
		bindRoot:         stmts.Statement("bind_root"),
		unbindRoot:       stmts.Statement("unbind_root"),
	}
}

// Verify prepares every statement against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// documentRoot reads the id of the organization's document root, or
// sql.ErrNoRows before its first write.
func (s *store) documentRoot(ctx context.Context, sess sqlate.Session, organizationID string) (string, error) {
	return s.documentRootRows.One(ctx, sess, query.Args{"organization_id": organizationID})
}

// findOrganization reads the organization, or sql.ErrNoRows.
func (s *store) findOrganization(ctx context.Context, sess sqlate.Session, organizationID string) error {
	_, err := s.organizationRows.One(ctx, sess, query.Args{"organization_id": organizationID})
	return err
}

// bind records the directory as the organization's document root.
func (s *store) bind(ctx context.Context, tx *sqlate.Tx, organizationID, directoryID string) error {
	_, err := s.bindRoot.Exec(ctx, tx, query.Args{"directory_id": directoryID, "organization_id": organizationID})
	return err
}

// unbind removes the owner row of the directory, if it is a document root.
func (s *store) unbind(ctx context.Context, tx *sqlate.Tx, directoryID string) error {
	_, err := s.unbindRoot.Exec(ctx, tx, query.Args{"directory_id": directoryID})
	return err
}

// listing is one of blobfs's directory listings, Directories' or Files':
// List reads a page by number and Continue the page past a cursor, both
// anchored on the directory's id. Both hide deleting rows unless given
// bfdata.IncludeDeleting.
type listing[T any] struct {
	List     func(context.Context, sqlate.Session, string, query.Directives, query.Page, ...bfdata.ListOption) (query.Collection[T], error)
	Continue func(context.Context, sqlate.Session, string, query.Directives, query.Cursor, int, ...bfdata.ListOption) (query.Collection[T], error)
}

// read runs the listing of the directory with id as the request addressed
// it, data.Read's lowering over blobfs's listing in place of a projection:
// past a cursor a previous page returned when the query names one, and by
// page number otherwise. Every refusal of the directives or the cursor is
// the request's error. Deleting rows are hidden, and the listing of a
// directory in a branch being deleted is not found: blobfs refuses it as
// blobfs.ErrDeleting, which the listing reports as the missing directory
// rather than a conflict: a mark is never undone, so nothing a client
// sends makes the directory listable again.
func (l listing[T]) read(ctx context.Context, sess sqlate.Session, id string, q web.Query) ([]T, web.Paging, error) {
	d := data.Directives(q)
	var c query.Collection[T]
	var err error
	if q.Cursor != "" {
		c, err = l.Continue(ctx, sess, id, d, query.Cursor(q.Cursor), q.Size)
	} else {
		c, err = l.List(ctx, sess, id, d, query.Page{Number: q.Page, Size: q.Size})
	}
	if errors.Is(err, blobfs.ErrDeleting) {
		return nil, web.Paging{}, fmt.Errorf("directory %s is being deleted: %w", id, blobfs.ErrNotFound)
	}
	return c.Items, data.Paging(c), err
}

// first reads the first page of the directory with id for a walk,
// uncounted, since the walk reads it again until it comes back empty. It
// lists every status: a file a stopped delete left deleting is the walk's
// to finish, and it would otherwise hold the directory not empty.
func (l listing[T]) first(ctx context.Context, sess sqlate.Session, id string) ([]T, error) {
	c, err := l.List(ctx, sess, id, query.Directives{Total: query.TotalNone}, query.Page{Number: 1, Size: walkPage}, bfdata.IncludeDeleting())
	return c.Items, err
}
