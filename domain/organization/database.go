package organization

import (
	"context"
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

//go:embed statements/*.sql
var files embed.FS

// store is the domain's SQL client: the statements of statements/ bound
// once to their typed handles, and the operations as methods named for
// their commands, so the file, the store method, the service method, and
// the route share one name. The tree lock is the data package's, taken by
// its registered name. The entities' tags are the scan and binding
// contract, so no scan function or Args literal is written for an entity
// here. It is the package's sole importer of the query library.
type store struct {
	db            *data.Database
	stmts         *query.Statements
	view          query.Projection[Organization]
	createRows    query.Rows[Identity]
	inSubtree     query.Rows[int64]
	editGuard     query.Guard
	transferGuard query.Guard
	deleteGuard   query.Guard
}

// newStore compiles the statements against the service's catalog, registers
// the inventory under the domain's name, and binds the handles. A compile
// failure is a wiring defect and panics; no I/O happens here.
func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	db.Register("organization", stmts)
	check := stmts.Statement("version")
	return &store{
		db:            db,
		stmts:         stmts,
		view:          stmts.Statement("organization_view").Project(query.Scanner[Organization]()),
		createRows:    stmts.Statement("create").Scan(query.Scanner[Identity]()),
		inSubtree:     stmts.Statement("in_subtree").Scan(query.Scalar[int64]),
		editGuard:     stmts.Statement("edit").Guarded(check, "version"),
		transferGuard: stmts.Statement("transfer").Guarded(check, "version"),
		deleteGuard:   stmts.Statement("delete").Guarded(check, "version"),
	}
}

// Verify prepares every statement and the projection's field contract
// against the live schema.
func (s *store) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts, s.view)
}

func (s *store) list(ctx context.Context, d query.Directives) ([]Organization, int, error) {
	return s.view.List(ctx, s.db, d)
}

func (s *store) find(ctx context.Context, field, value string) (Organization, error) {
	return s.view.One(ctx, s.db, field, value)
}

func (s *store) create(ctx context.Context, c CreateOrganization) (Identity, error) {
	return s.createRows.One(ctx, s.db, query.ArgsOf(c))
}

func (s *store) edit(ctx context.Context, id string, version int64, e EditOrganization) (Identity, error) {
	v, err := s.editGuard.Run(ctx, s.db, version, query.ArgsOf(e).With("id", id))
	return Identity{ID: id, Version: v}, err
}

// transfer is the domain's action: under the tree's lock, the cycle check
// walks the new parent's ancestor chain, then the guarded update moves
// parent_id. A nonexistent new parent falls through the walk to the
// foreign-key violation.
func (s *store) transfer(ctx context.Context, id string, version int64, t TransferOrganization) (Identity, error) {
	v, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (int64, error) {
		if err := s.db.Lock(ctx, tx, data.LockOrganizationTree); err != nil {
			return 0, err
		}
		if t.ParentID != nil {
			n, err := s.inSubtree.One(ctx, tx, query.Args{"node": id, "candidate": *t.ParentID})
			if err != nil {
				return 0, err
			}
			if n > 0 {
				return 0, fmt.Errorf("%w: %s is in the subtree of %s", ErrCycle, *t.ParentID, id)
			}
		}
		return s.transferGuard.Run(ctx, tx, version, query.ArgsOf(t).With("id", id))
	})
	return Identity{ID: id, Version: v}, err
}

func (s *store) delete(ctx context.Context, id string, version int64) error {
	_, err := s.deleteGuard.Run(ctx, s.db, version, query.Args{"id": id})
	return err
}
