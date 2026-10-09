package app

import (
	"fmt"
	"io"
	"log/slog"

	bfdata "github.com/standards-lab/blobfs/data"
	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/internal/config"
)

// defineInfrastructure defines the infrastructure nodes on g into n: the
// configuration cfg, the logger written to w, and the services the
// application is composed on. database is the database's lifecycle object,
// the provider's pool with its start, readiness, and shutdown, which the
// schema node administers; the domains never see it. storage is the object
// store itself, a lifecycle participant too, which the storage admin
// domain administers. sql is the database as the domains see it: the
// session over the same pool with the dialect, grouped with the pattern
// catalog every statement compiles against. files is the object storage as
// the domains see it: blobfs's store over that session, and the data
// package's adapter over the object store its keys name. The database is
// defined before the object store: the two share a layer, and the readiness
// probe reports a layer's checks in definition order.
//
// It constructs nothing, and no constructor opens a connection:
// connectivity belongs to each participant's Start, so a failed Build leaks
// none. This file is the one place a provider is named: the database's,
// the object store's, and blobfs's engine.
func defineInfrastructure(g *graph.Graph, n *Nodes, cfg *config.Config, w io.Writer) {
	n.Config = g.Define("config", func(*graph.Scope) (*config.Config, error) {
		return cfg, nil
	})
	n.Logger = g.Define("logger", newLogger(n, w))
	n.Database = g.Define("database", newDatabase(n))
	n.Storage = g.Define("storage", newStorage(n))
	n.SQL = g.Define("sql", newSQL(n))
	n.Files = g.Define("files", newFiles(n))
}

// newLogger constructs the service's logger over w from the config node's
// log block. The trace handler passes records through unchanged outside a
// span, so the wrap is unconditional and costs nothing when no trace is
// live.
func newLogger(n *Nodes, w io.Writer) func(*graph.Scope) (*slog.Logger, error) {
	return func(s *graph.Scope) (*slog.Logger, error) {
		return slog.New(observability.NewTraceHandler(logging.New(w, s.Use(n.Config).Log).Handler())), nil
	}
}

// newDatabase constructs the database pool from the config node's database
// block. It orders itself after telemetry, so the providers are installed
// before the pool starts and flushed after it closes.
func newDatabase(n *Nodes) func(*graph.Scope) (*database.DB, error) {
	return func(s *graph.Scope) (*database.DB, error) {
		s.After(n.Telemetry)
		return postgres.New(s.Use(n.Config).Database)
	}
}

// newStorage constructs the object store over its provider's client from
// the config node's storage block, ordered after telemetry as the database
// is.
func newStorage(n *Nodes) func(*graph.Scope) (*storage.Store, error) {
	return func(s *graph.Scope) (*storage.Store, error) {
		s.After(n.Telemetry)
		cfg := s.Use(n.Config).Storage
		client, err := azureblob.New(cfg)
		if err != nil {
			return nil, err
		}
		return storage.New(client, cfg), nil
	}
}

// newSQL groups the session over the database pool with the pattern
// catalog, built here, once: the library's namespace, blobfs's, and the
// application's.
func newSQL(n *Nodes) func(*graph.Scope) (*data.Database, error) {
	return func(s *graph.Scope) (*data.Database, error) {
		catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
		session := sqlate.Wrap(s.Use(n.Database).Conn(), pgdialect.Dialect{})
		return data.New(session, catalog), nil
	}
}

// newFiles constructs blobfs's store on the sql node's catalog and dialect,
// with its Postgres engine, and groups it with the object store its keys
// name.
func newFiles(n *Nodes) func(*graph.Scope) (*data.Storage, error) {
	return func(s *graph.Scope) (*data.Storage, error) {
		sql := s.Use(n.SQL)
		fs, err := bfdata.New(sql.Catalog, sql.Dialect(), bfdata.WithEngine(blobfspg.Engine))
		if err != nil {
			return nil, fmt.Errorf("blobfs: %w", err)
		}
		return data.NewStorage(sql, fs, s.Use(n.Storage)), nil
	}
}
