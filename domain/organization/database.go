package organization

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-web-sdk"
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
// here. It is the package's sole importer of the query library. The image
// statements are steps the logo's protocols in storage.go sequence around
// blobfs's, so their methods take the session the protocol hands them.
type store struct {
	db             *data.Database
	storage        *data.Storage
	logger         *slog.Logger
	stmts          *query.Statements
	view           query.Projection[Organization]
	createRows     query.Rows[Identity]
	inSubtree      query.Rows[int64]
	editGuard      query.Guard
	transferGuard  query.Guard
	deleteGuard    query.Guard
	versionRows    query.Rows[int64]
	activeLogoRows query.Rows[blobfs.File]
	attachImage    query.Statement
	detachImage    query.Statement
	seedRows       query.Rows[string]
	findSeededRows query.Rows[string]
}

// newStore compiles the statements against the service's catalog, registers
// the inventory under the domain's name, and binds the handles. A compile
// failure is a wiring defect and panics; no I/O happens here.
func newStore(db *data.Database, st *data.Storage, logger *slog.Logger) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	db.Register("organization", stmts)
	check := stmts.Statement("version")
	return &store{
		db:             db,
		storage:        st,
		logger:         logger,
		stmts:          stmts,
		view:           stmts.Statement("organization_view").Project(query.Scanner[Organization]()),
		createRows:     stmts.Statement("create").Scan(query.Scanner[Identity]()),
		inSubtree:      stmts.Statement("in_subtree").Scan(query.Scalar[int64]),
		editGuard:      stmts.Statement("edit").Guarded(check, "version"),
		transferGuard:  stmts.Statement("transfer").Guarded(check, "version"),
		deleteGuard:    stmts.Statement("delete").Guarded(check, "version"),
		versionRows:    check.Scan(query.Scalar[int64]),
		activeLogoRows: stmts.Statement("active_logo").Scan(query.Scanner[blobfs.File]()),
		attachImage:    stmts.Statement("attach_image"),
		detachImage:    stmts.Statement("detach_image"),
		seedRows:       stmts.Statement("seed").Scan(query.Scalar[string]),
		findSeededRows: stmts.Statement("find_seeded").Scan(query.Scalar[string]),
	}
}

// Verify prepares every statement and the projection's field contract
// against the live schema over sess; the store is a query.Verifier.
func (s *store) Verify(ctx context.Context, sess sqlate.Session) error {
	return query.Verify(ctx, sess, s.stmts, s.view)
}

func (s *store) list(ctx context.Context, q web.Query) ([]Organization, web.Paging, error) {
	c, err := data.Read(ctx, s.db, s.view, q)
	return c.Items, data.Paging(c), err
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

// exists reads the organization by its key alone, without the read
// model's lineage walk; a nonexistent organization is sql.ErrNoRows.
func (s *store) exists(ctx context.Context, sess sqlate.Session, id string) error {
	_, err := s.versionRows.One(ctx, sess, query.Args{"id": id})
	return err
}

// activeLogo reads the file the organization's active image binds,
// whatever its status, or sql.ErrNoRows.
func (s *store) activeLogo(ctx context.Context, sess sqlate.Session, organizationID string) (blobfs.File, error) {
	return s.activeLogoRows.One(ctx, sess, query.Args{"organization_id": organizationID})
}

// attach binds the file to the organization as its active image; the
// image it replaces must be removed first in the same transaction.
func (s *store) attach(ctx context.Context, tx *sqlate.Tx, organizationID, fileID string) error {
	_, err := s.attachImage.Exec(ctx, tx, query.Args{"organization_id": organizationID, "file_id": fileID})
	return err
}

// detach removes the image binding the file.
func (s *store) detach(ctx context.Context, tx *sqlate.Tx, fileID string) error {
	_, err := s.detachImage.Exec(ctx, tx, query.Args{"file_id": fileID})
	return err
}

// seed is the domain's seed contribution to the data package's named states:
// the organizations a state carries under "organizations", seeded by the
// store's own statements in the seed's transaction.
type seed struct{ store *store }

var _ data.Seed = seed{}

// Key names the organizations in a state file and in the seed's counts.
func (seed) Key() string { return "organizations" }

// Verifiers is the domain's store, whose statements include the seed's.
func (s seed) Verifiers() []query.Verifier { return []query.Verifier{s.store} }

// Apply inserts the tree in file order, each parent before its children,
// resolving the file's parent codes to ids as it goes, and returns how
// many rows it inserted.
func (s seed) Apply(ctx context.Context, tx *sqlate.Tx, raw json.RawMessage) (int, error) {
	rows, err := data.SeedRows[seedRow](raw)
	if err != nil {
		return 0, fmt.Errorf("seed organizations: %w", err)
	}
	ids := make(map[string]string, len(rows))
	inserted := 0
	for _, o := range rows {
		var parent any
		if o.Parent != "" {
			id, ok := ids[o.Parent]
			if !ok {
				return inserted, fmt.Errorf("seed organization %s: parent %q not seeded before it", o.Code, o.Parent)
			}
			parent = id
		}
		if _, dup := ids[o.Code]; dup {
			return inserted, fmt.Errorf("seed organization %s: code reused within the file", o.Code)
		}
		id, ok, err := s.store.seedOne(ctx, tx, parent, o)
		if err != nil {
			return inserted, fmt.Errorf("seed organization %s: %w", o.Code, err)
		}
		ids[o.Code] = id
		if ok {
			inserted++
		}
	}
	return inserted, nil
}

// seedOne seeds one organization or finds the one already there, returning
// its id and whether this call inserted it. The seed statement returns no
// row on conflict; sql.ErrNoRows is that signal.
func (s *store) seedOne(ctx context.Context, tx *sqlate.Tx, parent any, o seedRow) (id string, inserted bool, err error) {
	id, err = s.seedRows.One(ctx, tx, query.Args{"parent": parent, "code": o.Code, "name": o.Name})
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	id, err = s.findSeededRows.One(ctx, tx, query.Args{"parent": parent, "code": o.Code})
	if errors.Is(err, sql.ErrNoRows) {
		err = errors.New("neither inserted nor found")
	}
	return id, false, err
}
