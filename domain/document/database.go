package document

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client: the owner row's statements bound once
// to their typed handles, and the protocols of storage.go as its other
// methods. It is the package's sole importer of the query library; the
// lowering of a request's query onto blobfs's listings is the data
// package's, which storage.go calls. The owner-row statements are steps
// the protocols sequence around blobfs's, so their methods take the
// session the protocol hands them.
type store struct {
	db               *data.Database
	storage          *data.Storage
	stmts            *query.Statements
	documentRootRows query.Rows[string]
	organizationRows query.Rows[string]
	bindRoot         query.Statement
	seedOrgRows      query.Rows[string]
}

// newStore compiles the statements against the service's catalog, binds the
// handles, and registers the inventory under the domain's name with the
// store as its verifier, so the seeder's Verify checks it. A compile
// failure is a wiring defect and panics; no I/O happens here.
func newStore(db *data.Database, st *data.Storage) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	s := &store{
		db:               db,
		storage:          st,
		stmts:            stmts,
		documentRootRows: stmts.Statement("document_root").Scan(query.Scalar[string]),
		organizationRows: stmts.Statement("organization_exists").Scan(query.Scalar[string]),
		bindRoot:         stmts.Statement("bind_root"),
		seedOrgRows:      stmts.Statement("seed_organization").Scan(query.Scalar[string]),
	}
	db.Register("document", stmts, s)
	return s
}

// Verify prepares every statement against the live schema over sess,
// making the store a query.Verifier.
func (s *store) Verify(ctx context.Context, sess sqlate.Session) error {
	return query.Verify(ctx, sess, s.stmts)
}

// documentRoot reads the id of the organization's document root, or
// sql.ErrNoRows before its first write.
func (s *store) documentRoot(ctx context.Context, sess sqlate.Session, organizationID string) (string, error) {
	return s.documentRootRows.One(ctx, sess, query.Args{"organization_id": organizationID})
}

// organizationExists reads the organization, or sql.ErrNoRows.
func (s *store) organizationExists(ctx context.Context, sess sqlate.Session, organizationID string) error {
	_, err := s.organizationRows.One(ctx, sess, query.Args{"organization_id": organizationID})
	return err
}

// seedOrganization resolves the organization path a seed names, "/acme"
// or "/acme/engineering", one segment at a time from the root, or
// sql.ErrNoRows.
func (s *store) seedOrganization(ctx context.Context, sess sqlate.Session, path string) (string, error) {
	var id any
	for code := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		next, err := s.seedOrgRows.One(ctx, sess, query.Args{"parent": id, "code": code})
		if err != nil {
			return "", fmt.Errorf("organization %s: %w", path, err)
		}
		id = next
	}
	return id.(string), nil
}

// bind records the directory as the organization's document root.
func (s *store) bind(ctx context.Context, tx *sqlate.Tx, organizationID, directoryID string) error {
	_, err := s.bindRoot.Exec(ctx, tx, query.Args{"directory_id": directoryID, "organization_id": organizationID})
	return err
}
