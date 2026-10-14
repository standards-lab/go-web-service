package app

import (
	"context"
	"fmt"

	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/admin"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"

	dbadmin "github.com/standards-lab/go-web-service/admin/database"
	storageadmin "github.com/standards-lab/go-web-service/admin/storage"
	"github.com/standards-lab/go-web-service/data"
)

// defineAdmin defines the administrative nodes on g into n: the
// administrative counterpart of the domain layer, each service
// administering one infrastructure service over the library mechanisms it
// triggers. It constructs nothing.
//
// gate is the process's quiesce gate, which the database admin domain holds
// exclusively around every verb that changes the schema, so the sweep's
// passes and the relay's rows, which hold it shared, never run under one. schema is go-database's
// admin service, a lifecycle participant whose Start verifies and corrects
// the schema, checks every statement against it, and seeds, and which is
// its own readiness check. The storage admin domain administers the storage
// node's object store itself, read with Use: the same store as a second
// node would start and be checked twice.
func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Gate = g.Define("gate", func(*graph.Scope) (*reactor.Gate, error) {
		return new(reactor.Gate), nil
	})
	n.Schema = g.Define("schema", newSchema(n))
}

// newSchema constructs the database admin service over the database node's
// pool and the sql node's session and catalog. It administers the data
// package's content: the migration sets, go-messaging's and then blobfs's
// beneath the service's own, so startup applies them in that order and the
// admin verbs list and reset all three; the statements registry, which is
// the sql node; and the seeder, composed here from the domain nodes' seed
// contributions in the tables' dependency order. The stores the seeder
// verifies are the ones registered on the sql node (each domain's and
// blobfs's, and go-messaging's outbox and inbox, recorded here beside
// their set), not a list kept here; using the domain nodes builds them, and
// so registers them, first. Startup's own correction takes no gate, since
// it runs before the sweeper and the relay, which order themselves after
// the schema, start.
func newSchema(n *Nodes) func(*graph.Scope) (*admin.Service, error) {
	return func(s *graph.Scope) (*admin.Service, error) {
		sql := s.Use(n.SQL)
		logger := s.Use(n.Logger)

		messagingSet, err := msgpg.Migrations()
		if err != nil {
			return nil, fmt.Errorf("messaging migrations: %w", err)
		}
		blobfsSet, err := blobfspg.Migrations()
		if err != nil {
			return nil, fmt.Errorf("blobfs migrations: %w", err)
		}
		migrator, err := migrate.New(sql.DB, data.Migrations(messagingSet, blobfsSet), migrate.Options{Logger: logger})
		if err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}

		sql.Record(verifier(msgpg.Verify))

		org, doc := s.Use(n.Organization), s.Use(n.Document)
		seeder := data.NewSeeder(sql, org.Seed(), org.LogoSeed(), doc.Seed())

		return admin.New(s.Use(n.Database), sql.DB, migrator, sql.Catalog, admin.Options{
			Seed:     s.Use(n.Config).Admin.SeedState(),
			Seeder:   seeder,
			Registry: sql,
			Logger:   logger,
		}), nil
	}
}

// verifier adapts a library's verify function, such as go-messaging's
// postgres.Verify, to the store the seeder's verify runs.
type verifier func(ctx context.Context, sess sqlate.Session) error

// Verify runs v against the schema sess reaches.
func (v verifier) Verify(ctx context.Context, sess sqlate.Session) error { return v(ctx, sess) }

// mountAdmin builds the admin mount, /admin, with each admin domain's route
// group mounted into it: the schema's under the gate, and the storage
// node's object store. In production the mount belongs on its own
// listener, authenticated and unreachable from the public API's network
// path. Until the planned management listener exists, the mount serves on
// the API listener. Each group's error writer logs through the logger
// node.
func mountAdmin(s *graph.Scope, n *Nodes) *web.Group {
	logger := s.Use(n.Logger)
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(s.Use(n.Schema), s.Use(n.Gate), logger))
	g.Mount(storageadmin.Routes(s.Use(n.Storage), logger))
	return g
}
