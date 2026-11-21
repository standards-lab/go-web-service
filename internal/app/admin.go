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

func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Gate = g.Fresh[reactor.Gate]("gate")
	n.Schema = g.Define("schema", n.newSchema)
}

func (n *Nodes) newSchema(s *graph.Scope) (*admin.Service, error) {
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
		OnReset:  dbadmin.OnReset(sql.DB, s.Use(n.Runtime).Recorder),
	}), nil
}

type verifier func(ctx context.Context, sess sqlate.Session) error

// Verify runs v against the schema sess reaches.
func (v verifier) Verify(ctx context.Context, sess sqlate.Session) error { return v(ctx, sess) }

func mountAdmin(s *graph.Scope, n *Nodes) *web.Group {
	logger := s.Use(n.Logger)
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(s.Use(n.Schema), s.Use(n.Gate), logger))
	g.Mount(storageadmin.Routes(s.Use(n.Storage), logger))
	return g
}
