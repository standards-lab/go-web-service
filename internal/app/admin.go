package app

import (
	"fmt"

	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"

	dbadmin "github.com/standards-lab/go-web-service/admin/database"
	storageadmin "github.com/standards-lab/go-web-service/admin/storage"
	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// defineAdmin defines the administrative nodes on g into n: the
// administrative counterpart of the domain layer, each service
// administering one infrastructure service over the library mechanisms it
// triggers. It constructs nothing.
//
// gate is the process's quiesce gate, which the database admin domain holds
// exclusively around every verb that changes the schema, so the sweep's
// passes, which hold it shared, never run under one. schema is go-database's
// admin service, a lifecycle participant whose Start verifies and corrects
// the schema, checks every statement against it, and seeds, and which is
// its own readiness check. The storage admin domain administers the storage
// node's object store itself, read with Use: the same store as a second
// node would start and be checked twice.
func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Gate = g.Define("gate", func(*graph.Scope) (*sdk.Gate, error) {
		return new(sdk.Gate), nil
	})
	n.Schema = g.Define("schema", newSchema(n))
}

// newSchema constructs the database admin service over the database node's
// pool and the sql node's session and catalog. It administers the data
// package's content: the migration sets, blobfs's beneath the service's
// own; the statements registry, which is the sql node; and the seeder,
// composed here from the domain nodes' seed contributions in the tables'
// dependency order. The stores the seeder verifies are the ones registered
// on the sql node (each domain's and blobfs's), not a list kept here; using
// the domain nodes builds them, and so registers them, first. Startup's own
// correction takes no gate, since it runs before the sweeper, which orders
// itself after the schema, starts.
func newSchema(n *Nodes) func(*graph.Scope) (*admin.Service, error) {
	return func(s *graph.Scope) (*admin.Service, error) {
		sql := s.Use(n.SQL)
		logger := s.Use(n.Logger)

		blobfsSet, err := blobfspg.Migrations()
		if err != nil {
			return nil, fmt.Errorf("blobfs migrations: %w", err)
		}
		migrator, err := migrate.New(sql.DB, data.Migrations(blobfsSet), migrate.Options{Logger: logger})
		if err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}

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
