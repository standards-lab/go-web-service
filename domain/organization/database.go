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
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

//go:embed statements/*.sql
var files embed.FS

type store struct {
	db             *data.Database
	storage        *data.Storage
	logger         *slog.Logger
	events         *Recorder
	stmts          *query.Statements
	view           query.Projection[Organization]
	createRows     query.Rows[Identity]
	storedRows     query.Rows[OrganizationData]
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

func newStore(db *data.Database, st *data.Storage, logger *slog.Logger, events *Recorder) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	check := stmts.Statement("version")
	s := &store{
		db:             db,
		storage:        st,
		logger:         logger,
		events:         events,
		stmts:          stmts,
		view:           stmts.Statement("organization_view").Project(query.Scanner[Organization]()),
		createRows:     stmts.Statement("create").Scan(query.Scanner[Identity]()),
		storedRows:     stmts.Statement("stored").Scan(query.Scanner[OrganizationData]()),
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
	db.Register("organization", stmts, s)
	return s
}

// Verify prepares every statement and the projection's field contract
// against the live schema over sess, making the store a query.Verifier.
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
	return s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (Identity, error) {
		id, err := s.createRows.One(ctx, tx, query.ArgsOf(c))
		if err != nil {
			return Identity{}, err
		}
		return id, s.raiseStored(ctx, tx, q, OrganizationCreated, id.ID)
	}), nil)
}

func (s *store) edit(ctx context.Context, id string, version int64, e EditOrganization) (Identity, error) {
	v, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (int64, error) {
		v, err := s.editGuard.Run(ctx, tx, version, query.ArgsOf(e).With("id", id))
		if err != nil {
			return 0, err
		}
		return v, s.raiseStored(ctx, tx, q, OrganizationEdited, id)
	}), nil)
	return Identity{ID: id, Version: v}, err
}

func (s *store) transfer(ctx context.Context, id string, version int64, t TransferOrganization) (Identity, error) {
	v, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (int64, error) {
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
		v, err := s.transferGuard.Run(ctx, tx, version, query.ArgsOf(t).With("id", id))
		if err != nil {
			return 0, err
		}
		return v, s.raiseStored(ctx, tx, q, OrganizationTransferred, id)
	}), nil)
	return Identity{ID: id, Version: v}, err
}

func (s *store) delete(ctx context.Context, id string, version int64) error {
	_, err := s.db.Transact(ctx, s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (struct{}, error) {
		if _, err := s.deleteGuard.Run(ctx, tx, version, query.Args{"id": id}); err != nil {
			return struct{}{}, err
		}
		OrganizationDeleted.Raise(q, id, OrganizationDeletedData{OrganizationID: id, Version: version})
		return struct{}{}, nil
	}), nil)
	return err
}

func (s *store) exists(ctx context.Context, sess sqlate.Session, id string) error {
	_, err := s.versionRows.One(ctx, sess, query.Args{"id": id})
	return err
}

func (s *store) activeLogo(ctx context.Context, sess sqlate.Session, organizationID string) (blobfs.File, error) {
	return s.activeLogoRows.One(ctx, sess, query.Args{"organization_id": organizationID})
}

func (s *store) attach(ctx context.Context, tx *sqlate.Tx, organizationID, fileID string) error {
	_, err := s.attachImage.Exec(ctx, tx, query.Args{"organization_id": organizationID, "file_id": fileID})
	return err
}

func (s *store) detach(ctx context.Context, tx *sqlate.Tx, fileID string) error {
	_, err := s.detachImage.Exec(ctx, tx, query.Args{"file_id": fileID})
	return err
}

type seed struct{ store *store }

var _ data.Seed = seed{}

// Key names the organizations in a state file and in the seed's counts.
func (seed) Key() string { return "organizations" }

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

func (s *store) seedOne(ctx context.Context, tx *sqlate.Tx, parent any, o seedRow) (id string, inserted bool, err error) {
	type seeded struct {
		id       string
		inserted bool
	}
	r, err := s.events.Emit(ctx, func(tx *sqlate.Tx, q *event.Queue) (seeded, error) {
		id, err := s.seedRows.One(ctx, tx, query.Args{"parent": parent, "code": o.Code, "name": o.Name})
		if err == nil {
			return seeded{id, true}, s.raiseStored(ctx, tx, q, OrganizationCreated, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return seeded{}, err
		}
		id, err = s.findSeededRows.One(ctx, tx, query.Args{"parent": parent, "code": o.Code})
		if errors.Is(err, sql.ErrNoRows) {
			err = errors.New("neither inserted nor found")
		}
		return seeded{id, false}, err
	})(tx)
	return r.id, r.inserted, err
}
