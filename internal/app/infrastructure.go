package app

import (
	"fmt"
	"log/slog"

	bfdata "github.com/standards-lab/blobfs/data"
	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-messaging/nats"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

// defineInfrastructure defines the infrastructure nodes on g into n: the
// lifecycle config the Coordinator is configured from, the logger written
// to the writer node, and the services the application is composed on,
// each read from the config node New defined. database is the database's
// lifecycle object, the provider's pool with its start, readiness, and
// shutdown, which the schema node administers; the domains never see it. storage is the object
// store itself, a lifecycle participant too, which the storage admin
// domain administers. broker is the NATS JetStream broker the messaging
// runtime (reactors.go) publishes through, a lifecycle participant whose
// readiness is its connection's. sql is the database as the domains see
// it: the session over the same pool with the dialect, grouped with the
// pattern catalog every statement compiles against. files is the object
// storage as the domains see it: blobfs's store over that session, and the
// data package's adapter over the object store its keys name. The
// database, the object store, and the broker are defined in that order:
// the three share a layer, and the readiness probe reports a layer's
// checks in definition order.
//
// It constructs nothing, and no constructor opens a connection:
// connectivity belongs to each participant's Start, so a failed Build leaks
// none. This file names the providers the connections are built on: the
// database's, the object store's, the broker's, and blobfs's engine. The
// engines are named in two other layer files, the schema (admin.go), which
// declares their migration sets, and the runtime (reactors.go), which takes
// go-messaging's outbox and inbox, so the package is the one place a
// provider is named.
func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", n.newLifecycleConfig)
	n.Logger = g.Define("logger", n.newLogger)
	n.Database = g.Define("database", n.newDatabase, graph.After(n.Telemetry))
	n.Storage = g.Define("storage", n.newStorage, graph.After(n.Telemetry))
	n.Broker = g.Define("broker", n.newBroker, graph.After(n.Telemetry))
	n.SQL = g.Define("sql", n.newSQL)
	n.Files = g.Define("files", n.newFiles)
}

// newLifecycleConfig returns the config node's lifecycle block, the
// configuration [App.Run] hands the lifecycle Coordinator.
func (n *Nodes) newLifecycleConfig(s *graph.Scope) (lifecycle.Config, error) {
	return s.Use(n.Config).Config, nil
}

// newLogger constructs the service's logger over the writer node from the
// config node's log block. The trace handler passes records through
// unchanged outside a span, so the wrap is unconditional and costs nothing
// when no trace is live.
func (n *Nodes) newLogger(s *graph.Scope) (*slog.Logger, error) {
	return slog.New(observability.NewTraceHandler(logging.New(s.Use(n.Writer), s.Use(n.Config).Log).Handler())), nil
}

// newDatabase constructs the database pool from the config node's database
// block. Its Define orders it after telemetry, so the providers are
// installed before the pool starts and flushed after it closes.
func (n *Nodes) newDatabase(s *graph.Scope) (*database.DB, error) {
	return postgres.New(s.Use(n.Config).Database)
}

// newStorage constructs the object store over its provider's client from
// the config node's storage block. Its Define orders it after telemetry, as
// the database's does.
func (n *Nodes) newStorage(s *graph.Scope) (*storage.Store, error) {
	cfg := s.Use(n.Config).Storage
	client, err := azureblob.New(cfg)
	if err != nil {
		return nil, err
	}
	return storage.New(client, cfg), nil
}

// newBroker constructs the NATS broker from the config node's nats block.
// Its Define orders it after telemetry, as the connections' do. Its Start
// connects, and provisions the stream the block's owner switch makes it own;
// a broker whose server is down at startup fails startup, and one whose
// connection drops later reconnects without limit, unready until it is back.
func (n *Nodes) newBroker(s *graph.Scope) (*nats.Broker, error) {
	return nats.New(s.Use(n.Config).NATS)
}

// newSQL groups the session over the database pool with the pattern
// catalog, built here, once: the library's namespace, blobfs's, and the
// application's.
func (n *Nodes) newSQL(s *graph.Scope) (*data.Database, error) {
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	session := sqlate.Wrap(s.Use(n.Database).Conn(), pgdialect.Dialect{})
	return data.New(session, catalog), nil
}

// newFiles constructs blobfs's store on the sql node's catalog and dialect,
// with its Postgres engine, and groups it with the object store its keys
// name.
func (n *Nodes) newFiles(s *graph.Scope) (*data.Storage, error) {
	sql := s.Use(n.SQL)
	fs, err := bfdata.New(sql.Catalog, sql.Dialect(), bfdata.Options{Engine: blobfspg.Engine})
	if err != nil {
		return nil, fmt.Errorf("blobfs: %w", err)
	}
	return data.NewStorage(sql, fs, s.Use(n.Storage)), nil
}
