package data

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
)

//go:embed seeds/*.json seeds/fixtures
var seedFiles embed.FS

// Contribution is one domain's seed contribution to the named states: the
// rows a state file carries under Key. The domain declares it, since the
// tables and their rows are the domain's, and the composition root hands
// it to [NewSeeder], so this package reads the states without naming any
// domain's table. A contribution is either a [Seed], rows applied in the
// seed's transaction, or a [FileSeed], files written through the storage
// protocols after that transaction commits; it is never both.
type Contribution interface {
	// Key names the contribution's rows in a state file and its count in
	// the seed's result.
	Key() string
	// Verify prepares the statements the contribution runs against the
	// live schema.
	Verify(ctx context.Context) error
}

// Seed is a contribution of rows alone, applied by the domain's own
// statements in the seed's one transaction.
type Seed interface {
	Contribution
	// Apply inserts rows, the state's JSON under Key, in tx, leaving a row
	// already there as it is, and returns how many rows it inserted. Rows
	// decode strictly (SeedRows), so an unknown field is a defect in the
	// file.
	Apply(ctx context.Context, tx *sqlate.Tx, rows json.RawMessage) (int, error)
}

// FileSeed is a contribution of stored files: rows whose objects the
// object store holds. A file's write cannot join the seed's transaction,
// since blobfs's two-phase write commits the pending row before any byte
// is stored and puts the object outside any transaction, so the seeder
// runs every FileSeed after the row transaction commits, when the rows the
// files name already stand.
type FileSeed interface {
	Contribution
	// Write stores the files rows declare, the state's JSON under Key,
	// each through the shared write protocol (Storage.Ensure), and
	// returns how many it stored. A file already there is left as it is,
	// and every row carries its id, so a rerun finds each file and a
	// reset writes it again under the same key. fixtures holds the bytes
	// a row names by path; rows decode strictly (SeedRows).
	Write(ctx context.Context, rows json.RawMessage, fixtures fs.FS) (int, error)
}

// Seeder is the seed operation over the named states, composed from the
// domains' contributions. A state is one file under seeds/, the data a
// deployment or a scenario starts from, keyed by contribution; each
// contribution applies its own key's rows. Files a state names by path
// sit under seeds/fixtures/, embedded with the states. The admin service
// owns the policy of which set applies and when; Seeder owns how. Seeder
// is the admin service's Seeder.
type Seeder struct {
	db       *Database
	all      []Contribution
	rows     []Seed
	files    []FileSeed
	fixtures fs.FS
}

// NewSeeder composes the seed operation from the domains' contributions,
// the rows applied in the order given, then the files in the order given,
// which the composition root makes the tables' dependency order. Two
// contributions under one key, or one that is neither a Seed nor a
// FileSeed, or both, are wiring defects and panic.
func NewSeeder(db *Database, contributions ...Contribution) *Seeder {
	fixtures, err := fs.Sub(seedFiles, "seeds/fixtures")
	if err != nil {
		panic(fmt.Sprintf("seeds: %v", err)) // the directory is embedded
	}
	s := &Seeder{db: db, all: contributions, fixtures: fixtures}
	keys := make(map[string]bool, len(contributions))
	for _, c := range contributions {
		if keys[c.Key()] {
			panic("data: two seed contributions under " + c.Key())
		}
		keys[c.Key()] = true
		rows, isRows := c.(Seed)
		files, isFiles := c.(FileSeed)
		switch {
		case isRows && isFiles:
			panic("data: the seed contribution " + c.Key() + " is both a Seed and a FileSeed")
		case isRows:
			s.rows = append(s.rows, rows)
		case isFiles:
			s.files = append(s.files, files)
		default:
			panic("data: the seed contribution " + c.Key() + " is neither a Seed nor a FileSeed")
		}
	}
	return s
}

// Verify prepares the package's statements and every contribution's
// against the live schema.
func (s *Seeder) Verify(ctx context.Context) error {
	errs := []error{s.db.Verify(ctx)}
	for _, c := range s.all {
		errs = append(errs, c.Verify(ctx))
	}
	return errors.Join(errs...)
}

// States lists the embedded state files by name, sorted; the fixtures
// directory beside them is not a state.
func (s *Seeder) States() []string {
	entries, err := fs.ReadDir(seedFiles, "seeds")
	if err != nil {
		panic(fmt.Sprintf("seeds: %v", err)) // the directory is embedded
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	return names
}

// Seed applies the named state: every Seed's rows in one transaction, in
// the order the contributions were given, then, once it commits, every
// FileSeed's files, each through the storage protocols. It is idempotent,
// leaving an existing row or file as it is, so it runs at every startup of
// an environment that names a set and on demand from the admin mount. The
// counts are what this run inserted, rows or files, for every
// contribution; one the state does not carry, or a seeded database,
// reports zero. A key no contribution reads is a defect in the file,
// refused before any I/O; the refusal names the first such key by name.
//
// A failure in the transaction rolls every row back, and no file is
// written. A failure writing files leaves the committed rows and the
// files stored before it, each file's write abandoned by the protocol when
// it fails partway, so a rerun converges; the counts returned beside the
// error are what the run stored before it stopped.
func (s *Seeder) Seed(ctx context.Context, name string) (admin.Seeded, error) {
	var st map[string]json.RawMessage
	if err := readSeed(name, &st); err != nil {
		return nil, err
	}
	for _, key := range slices.Sorted(maps.Keys(st)) {
		if !slices.ContainsFunc(s.all, func(c Contribution) bool { return c.Key() == key }) {
			return nil, fmt.Errorf("seed %s: no contribution reads %q", name, key)
		}
	}
	n, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (admin.Seeded, error) {
		n := make(admin.Seeded, len(s.all))
		for _, c := range s.all {
			n[c.Key()] = 0
		}
		for _, c := range s.rows {
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
	if err != nil {
		return n, err
	}
	for _, c := range s.files {
		rows, ok := st[c.Key()]
		if !ok {
			continue
		}
		stored, err := c.Write(ctx, rows, s.fixtures)
		n[c.Key()] = stored
		if err != nil {
			return n, fmt.Errorf("seed %s: %w", c.Key(), err)
		}
	}
	return n, nil
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
