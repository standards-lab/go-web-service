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

func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", n.newLifecycleConfig)
	n.Logger = g.Define("logger", n.newLogger)
	n.Database = g.Define("database", n.newDatabase, graph.After(n.Telemetry))
	n.Storage = g.Define("storage", n.newStorage, graph.After(n.Telemetry))
	n.Broker = g.Define("broker", n.newBroker, graph.After(n.Telemetry))
	n.SQL = g.Define("sql", n.newSQL)
	n.Files = g.Define("files", n.newFiles)
}

func (n *Nodes) newLifecycleConfig(s *graph.Scope) (lifecycle.Config, error) {
	return s.Use(n.Config).Config, nil
}

func (n *Nodes) newLogger(s *graph.Scope) (*slog.Logger, error) {
	return slog.New(observability.NewTraceHandler(logging.New(s.Use(n.Writer), s.Use(n.Config).Log).Handler())), nil
}

func (n *Nodes) newDatabase(s *graph.Scope) (*database.DB, error) {
	return postgres.New(s.Use(n.Config).Database)
}

func (n *Nodes) newStorage(s *graph.Scope) (*storage.Store, error) {
	cfg := s.Use(n.Config).Storage
	client, err := azureblob.New(cfg)
	if err != nil {
		return nil, err
	}
	return storage.New(client, cfg), nil
}

func (n *Nodes) newBroker(s *graph.Scope) (*nats.Broker, error) {
	return nats.New(s.Use(n.Config).NATS)
}

func (n *Nodes) newSQL(s *graph.Scope) (*data.Database, error) {
	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	session := sqlate.Wrap(s.Use(n.Database).Conn(), pgdialect.Dialect{})
	return data.New(session, catalog), nil
}

func (n *Nodes) newFiles(s *graph.Scope) (*data.Storage, error) {
	sql := s.Use(n.SQL)
	fs, err := bfdata.New(sql.Catalog, sql.Dialect(), bfdata.Options{Engine: blobfspg.Engine})
	if err != nil {
		return nil, fmt.Errorf("blobfs: %w", err)
	}
	return data.NewStorage(sql, fs, s.Use(n.Storage)), nil
}
