package data

import (
	"sort"
	"sync"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Database is the database as a domain sees it: the session, and the
// catalog of every pattern namespace the composition root registered. A
// domain compiles its statements against Catalog and runs them through
// DB; it defines no patterns of its own. It also keeps the statements
// registry: every domain registers its compiled inventory at wiring, so
// the admin service can walk the whole service's SQL the way the catalog
// lists its patterns. Verification stays each domain's own lifecycle
// stage.
type Database struct {
	*sqlate.DB
	Catalog *query.Catalog

	mu       sync.Mutex
	registry map[string]*query.Statements
}

// New groups a session with the catalog its statements compile against.
func New(db *sqlate.DB, catalog *query.Catalog) *Database {
	return &Database{DB: db, Catalog: catalog, registry: map[string]*query.Statements{}}
}

// Register records stmts under name, a domain's, at wiring; registering a
// name twice is a wiring defect and panics.
func (d *Database) Register(name string, stmts *query.Statements) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, dup := d.registry[name]; dup {
		panic("data: statements registered twice under " + name)
	}
	d.registry[name] = stmts
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
