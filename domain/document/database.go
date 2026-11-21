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

type store struct {
	db               *data.Database
	storage          *data.Storage
	events           *Recorder
	stmts            *query.Statements
	documentRootRows query.Rows[string]
	organizationRows query.Rows[string]
	bindRoot         query.Statement
	seedOrgRows      query.Rows[string]
}

func newStore(db *data.Database, st *data.Storage, events *Recorder) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	s := &store{
		db:               db,
		storage:          st,
		events:           events,
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

func (s *store) documentRoot(ctx context.Context, sess sqlate.Session, organizationID string) (string, error) {
	return s.documentRootRows.One(ctx, sess, query.Args{"organization_id": organizationID})
}

func (s *store) organizationExists(ctx context.Context, sess sqlate.Session, organizationID string) error {
	_, err := s.organizationRows.One(ctx, sess, query.Args{"organization_id": organizationID})
	return err
}

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

func (s *store) bind(ctx context.Context, tx *sqlate.Tx, organizationID, directoryID string) error {
	_, err := s.bindRoot.Exec(ctx, tx, query.Args{"directory_id": directoryID, "organization_id": organizationID})
	return err
}
