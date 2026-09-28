package data

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
)

//go:embed seeds/*.json
var seedFiles embed.FS

// Seed is one domain's contribution to the named states: the rows a state
// file carries under Key, applied by the domain's own statements. The
// domain declares it, since the table and its rows are the domain's, and
// the composition root hands it to [NewSeeder], so this package reads the
// states without naming any domain's table.
type Seed interface {
	// Key names the contribution's rows in a state file and its count in
	// the seed's result.
	Key() string
	// Verify prepares the statements Apply runs against the live schema.
	Verify(ctx context.Context) error
	// Apply inserts rows, the state's JSON under Key, in tx, leaving a row
	// already there as it is, and returns how many rows it inserted. Rows
	// decode strictly (SeedRows), so an unknown field is a defect in the
	// file.
	Apply(ctx context.Context, tx *sqlate.Tx, rows json.RawMessage) (int, error)
}

// Seeder is the seed operation over the named states, composed from the
// domains' contributions. A state is one file under seeds/, the data a
// deployment or a scenario starts from, keyed by contribution; each
// contribution applies its own key's rows. The admin service owns the
// policy of which set applies and when; Seeder owns how. Seeder is the
// admin service's Seeder.
type Seeder struct {
	db    *Database
	seeds []Seed
}

// NewSeeder composes the seed operation from the domains' contributions,
// applied in the order given, which the composition root makes the tables'
// dependency order. Two contributions under one key are a wiring defect
// and panic.
func NewSeeder(db *Database, seeds ...Seed) *Seeder {
	keys := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		if keys[s.Key()] {
			panic("data: two seed contributions under " + s.Key())
		}
		keys[s.Key()] = true
	}
	return &Seeder{db: db, seeds: seeds}
}

// Verify prepares the package's statements and every contribution's
// against the live schema.
func (s *Seeder) Verify(ctx context.Context) error {
	errs := []error{s.db.Verify(ctx)}
	for _, c := range s.seeds {
		errs = append(errs, c.Verify(ctx))
	}
	return errors.Join(errs...)
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

// Seed applies the named state in one transaction, each contribution's
// rows by its own Apply in the order the contributions were given. It is
// idempotent through each table's unique constraint, an existing row
// being left as it is, so it runs at every startup of an environment that
// names a set and on demand from the admin mount. The counts are the rows
// this run inserted, for every contribution; one the state does not
// carry, or a seeded database, reports zero. A key no contribution reads
// is a defect in the file, refused before any I/O.
func (s *Seeder) Seed(ctx context.Context, name string) (admin.Seeded, error) {
	var st map[string]json.RawMessage
	if err := readSeed(name, &st); err != nil {
		return nil, err
	}
	for key := range st {
		if !slices.ContainsFunc(s.seeds, func(c Seed) bool { return c.Key() == key }) {
			return nil, fmt.Errorf("seed %s: no contribution reads %q", name, key)
		}
	}
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (admin.Seeded, error) {
		n := make(admin.Seeded, len(s.seeds))
		for _, c := range s.seeds {
			n[c.Key()] = 0
			rows, ok := st[c.Key()]
			if !ok {
				continue
			}
			inserted, err := c.Apply(ctx, tx, rows)
			n[c.Key()] = inserted
			if err != nil {
				return n, err
			}
		}
		return n, nil
	})
}

// SeedRows decodes a contribution's rows strictly: an unknown field is a
// defect in the file, not data to ignore.
func SeedRows[T any](rows json.RawMessage) ([]T, error) {
	var out []T
	dec := json.NewDecoder(bytes.NewReader(rows))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// readSeed decodes seeds/<name>.json, an object of rows by contribution
// key. A name with no file is [admin.ErrUnknownState].
func readSeed(name string, v any) error {
	f, err := seedFiles.Open("seeds/" + name + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", admin.ErrUnknownState, name)
	}
	if err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	if err := json.NewDecoder(f).Decode(v); err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	return nil
}
