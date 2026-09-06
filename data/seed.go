package data

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

//go:embed seeds/*.json
var seedFiles embed.FS

//go:embed statements/*.sql
var seedStatements embed.FS

// Seeder is the seed operation bound to its statements: the seed's insert
// and lookup, authored files under statements/ (the native tier, ON
// CONFLICT and RETURNING, declared in their headers; seeds never port),
// compiled once against the catalog and held as handles. The admin
// service owns the policy of when it runs; Seeder owns how. Seeder is the
// admin service's Seeder.
type Seeder struct {
	db      *sqlate.DB
	stmts   *query.Statements
	seedOrg query.Rows[string]
	findOrg query.Rows[string]
}

// NewSeeder compiles the seed statements against db's catalog, registers
// them under "seed", and binds the handles; a compile failure is a wiring
// defect and panics.
func NewSeeder(db *Database) *Seeder {
	stmts := db.Catalog.MustCompile(seedStatements, "statements", db.Dialect())
	db.Register("seed", stmts)
	return &Seeder{
		db:      db.DB,
		stmts:   stmts,
		seedOrg: stmts.Statement("seed_organization").Scan(query.Scalar[string]),
		findOrg: stmts.Statement("find_organization").Scan(query.Scalar[string]),
	}
}

// Verify prepares every seed statement against the live schema.
func (s *Seeder) Verify(ctx context.Context) error {
	return query.Verify(ctx, s.db, s.stmts)
}

// organizationSeed is one row of seeds/organizations.json, the domain's
// reference data for development and test in the vocabulary of its API:
// an organization names its parent by code, the empty code being the root.
type organizationSeed struct {
	Parent string `json:"parent"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

// Seed loads the embedded seed files in one transaction, each table by its
// own seed function in dependency order. It is idempotent through each
// table's unique constraint, an existing row being left as it is, so it
// runs at every startup of an environment that enables it and on demand
// from the admin mount. The counts are the rows this run inserted, keyed
// by table; a seeded database reports zeros.
func (s *Seeder) Seed(ctx context.Context) (admin.Seeded, error) {
	var orgs []organizationSeed
	if err := readSeed("organizations", &orgs); err != nil {
		return nil, err
	}
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (admin.Seeded, error) {
		n, err := s.seedOrganizations(ctx, tx, orgs)
		return admin.Seeded{"organizations": n}, err
	})
}

// seedOrganizations inserts the tree in file order, each parent before its
// children, resolving the file's parent codes to ids as it goes, and
// returns how many rows it inserted.
func (s *Seeder) seedOrganizations(ctx context.Context, tx *sqlate.Tx, rows []organizationSeed) (int, error) {
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
		id, ok, err := s.seedOrganization(ctx, tx, parent, o)
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

// seedOrganization seeds one organization or finds the one already there,
// returning its id and whether this call inserted it. The statement returns
// no row on conflict; sql.ErrNoRows is that signal.
func (s *Seeder) seedOrganization(ctx context.Context, tx *sqlate.Tx, parent any, o organizationSeed) (id string, inserted bool, err error) {
	id, err = s.seedOrg.One(ctx, tx, query.Args{"parent": parent, "code": o.Code, "name": o.Name})
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	id, err = s.findOrg.One(ctx, tx, query.Args{"parent": parent, "code": o.Code})
	if errors.Is(err, sql.ErrNoRows) {
		err = errors.New("neither inserted nor found")
	}
	return id, false, err
}

// readSeed decodes seeds/<name>.json strictly: an unknown field is a
// defect in the file, not data to ignore.
func readSeed(name string, v any) error {
	f, err := seedFiles.Open("seeds/" + name + ".json")
	if err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	return nil
}
