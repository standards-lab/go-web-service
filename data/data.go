package data

import (
	"embed"
	"slices"
	"sort"
	"sync"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

//go:embed statements/*.sql
var files embed.FS

// Database is the database as a domain sees it: the session, and the
// catalog of every pattern namespace the composition root registered. A
// domain compiles its statements against Catalog and runs them through
// DB; it defines no patterns of its own. It also keeps the statements
// registry: every domain registers its compiled inventory at wiring, so
// the admin service can walk the whole service's SQL the way the catalog
// lists its patterns. Each registration, and [NewStorage]'s for blobfs's
// store, also records the store's verifier, which [Seeder.Verify] runs, so
// a store that runs statements cannot be left out of the startup check.
// The package's own statement, under
// statements/, is the lock; it compiles once here and registers under
// "data". The seed's statements are the domains' own (Seed).
type Database struct {
	*sqlate.DB
	Catalog *query.Catalog

	stmts *query.Statements
	lock  query.Statement

	mu        sync.Mutex
	registry  map[string]*query.Statements
	verifiers []query.Verifier
}

// New groups a session with the catalog its statements compile against
// and compiles the package's own statements; a compile failure is a
// wiring defect and panics. No I/O happens here.
func New(db *sqlate.DB, catalog *query.Catalog) *Database {
	d := &Database{DB: db, Catalog: catalog, registry: map[string]*query.Statements{}}
	d.stmts = catalog.MustCompile(files, "statements", db.Dialect())
	d.lock = d.stmts.Statement("lock")
	d.Register("data", d.stmts, d.stmts)
	return d
}

// Register records stmts under name, a domain's, at wiring, and verifier,
// the store that checks them (and any projection over them) against the
// live schema, for [Seeder.Verify]. Registration never closes: a store
// registered after the seeder is built is verified by its next Verify.
// Registering a name twice is a wiring defect and panics.
func (d *Database) Register(name string, stmts *query.Statements, verifier query.Verifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, dup := d.registry[name]; dup {
		panic("data: statements registered twice under " + name)
	}
	d.registry[name] = stmts
	d.verifiers = append(d.verifiers, verifier)
}

// verify records a verifier that has no statements registry entry, as
// blobfs's store, whose statements are its own.
func (d *Database) verify(v query.Verifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.verifiers = append(d.verifiers, v)
}

// recorded returns the verifiers registered so far, in registration order.
func (d *Database) recorded() []query.Verifier {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.verifiers)
}

// Registry returns the statements registry in name order. Database is the
// admin service's Registry.
func (d *Database) Registry() []admin.Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]admin.Entry, 0, len(d.registry))
	for name, stmts := range d.registry {
		out = append(out, admin.Entry{Name: name, Statements: stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
