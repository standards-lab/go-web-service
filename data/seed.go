package data

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

//go:embed seeds/*.json
var seedFiles embed.FS

// Seeder is the seed operation over the named states, bound to its
// statements: the seed's insert and lookup. Those statements are authored
// files under statements/, declaring the native tier (ON CONFLICT and
// RETURNING) in their headers since seeds never port, and are held as
// handles over the package's compiled inventory. A state is one file under
// seeds/, the data a
// deployment or a scenario starts from, keyed by table. The admin service
// owns the policy of which set applies and when; Seeder owns how. Seeder
// is the admin service's Seeder.
type Seeder struct {
	db      *Database
	seedOrg query.Rows[string]
	findOrg query.Rows[string]
}

// NewSeeder binds the seed handles from db's statements.
func NewSeeder(db *Database) *Seeder {
	return &Seeder{
		db:      db,
		seedOrg: db.stmts.Statement("seed_organization").Scan(query.Scalar[string]),
		findOrg: db.stmts.Statement("find_organization").Scan(query.Scalar[string]),
	}
}

// Verify prepares the package's statements against the live schema.
func (s *Seeder) Verify(ctx context.Context) error {
	return s.db.Verify(ctx)
}

// state is one file under seeds/, decoded strictly: one field per table
// the seeder knows, so a key for a table it does not is a defect in the
// file, and a domain that joins the seed adds its field here.
type state struct {
	Organizations []organizationSeed `json:"organizations"`
}

// organizationSeed is one row of a state's organizations, in the
// vocabulary of the domain's API: an organization names its parent by
// code, the empty code being the root.
type organizationSeed struct {
	Parent string `json:"parent"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

// States lists the embedded state files by name, sorted.
func (s *Seeder) States() []string {
	entries, err := fs.ReadDir(seedFiles, "seeds")
	if err != nil {
		panic(fmt.Sprintf("seeds: %v", err)) // the directory is embedded
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), path.Ext(e.Name())))
	}
	return names
}

// Seed applies the named state's set in one transaction, each table by
// its own seed function in dependency order. It is idempotent through
// each table's unique constraint, an existing row being left as it is, so
// it runs at every startup of an environment that names a set and on
// demand from the admin mount. The counts are the rows this run inserted,
// for every table the seeder knows; a table the state does not carry, or
// a seeded database, reports zero.
func (s *Seeder) Seed(ctx context.Context, name string) (admin.Seeded, error) {
	var st state
	if err := readSeed(name, &st); err != nil {
		return nil, err
	}
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (admin.Seeded, error) {
		n, err := s.seedOrganizations(ctx, tx, st.Organizations)
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
// defect in the file, not data to ignore. A name with no file is
// [admin.ErrUnknownState].
func readSeed(name string, v any) error {
	f, err := seedFiles.Open("seeds/" + name + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", admin.ErrUnknownState, name)
	}
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
