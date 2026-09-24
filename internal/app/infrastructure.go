package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	bfdata "github.com/standards-lab/blobfs/data"
	blobfspg "github.com/standards-lab/blobfs/postgres"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/internal/config"
)

// Infrastructure holds the services the application is composed on, one
// concrete field per service. DB is the database's lifecycle object, the
// provider's pool with its start, readiness, and shutdown, which the admin
// service administers; the domains never see it. SQL is the database as
// the domains see it: the session over the same pool with the dialect,
// grouped with the pattern catalog every statement compiles against.
// Storage is the object store as the domains see it: blobfs's store over
// the same session, and the started object store its keys name. Sets are
// the migration sets the admin service's migrator runs, blobfs's beneath
// the service's own. The struct stops at the composition root: the layer
// files read its fields, and a package receives its dependencies as
// constructor parameters, never the struct itself.
type Infrastructure struct {
	Logger  *slog.Logger
	DB      *database.DB
	SQL     *data.Database
	Storage *data.Storage
	Sets    []migrate.Set
}

// storageStage verifies blobfs's statements against the migrated schema,
// once the schema service at admin.Stage has corrected it, beside the
// domains that verify their own.
const storageStage = admin.Stage + 1

// newInfrastructure constructs the infrastructure services in one place, in
// dependency order, each registering on lc where it is built as a
// lifecycle.Service with the stage that places it in the startup order, so
// a service cannot exist without a startup, shutdown, or readiness
// declaration. The database and the object store register at stage 0 with
// their readiness checks, so both are started before the schema service
// seeds objects. Construction opens nothing: connectivity belongs to a
// service's Start. The pattern catalog is built here, once: the library's
// namespace, blobfs's, and the application's; a port adds the engine's
// overlay beside them. This file is the one place a provider is named:
// the database's, the object store's, and blobfs's engine with its
// migration set.
func newInfrastructure(
	w io.Writer,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Infrastructure, error) {
	// The trace handler passes records through unchanged outside a span, so
	// the wrap is unconditional and costs nothing when no trace is live.
	logger := slog.New(observability.NewTraceHandler(logging.New(w, cfg.Log).Handler()))

	db, err := postgres.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	lc.Add(lifecycle.Service{
		Name:     "database",
		Stage:    0,
		Start:    db.Start,
		Shutdown: db.Shutdown,
		Check:    db,
	})

	client, err := azureblob.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	objects := storage.New(client, cfg.Storage)
	lc.Add(lifecycle.Service{
		Name:     "storage",
		Stage:    0,
		Start:    objects.Start,
		Shutdown: objects.Shutdown,
		Check:    objects,
	})

	catalog := query.MustCatalog(query.Patterns(), bfdata.Patterns(), data.Patterns())
	session := sqlate.Wrap(db.Conn(), pgdialect.Dialect{})

	fs, err := bfdata.New(catalog, session.Dialect(), bfdata.WithEngine(blobfspg.Engine))
	if err != nil {
		return nil, fmt.Errorf("blobfs: %w", err)
	}
	lc.Add(lifecycle.Service{
		Name:  "blobfs",
		Stage: storageStage,
		Start: func(ctx context.Context) error { return fs.Verify(ctx, session) },
	})
	blobfsSet, err := blobfspg.Migrations()
	if err != nil {
		return nil, fmt.Errorf("blobfs migrations: %w", err)
	}

	return &Infrastructure{
		Logger:  logger,
		DB:      db,
		SQL:     data.New(session, catalog),
		Storage: data.NewStorage(fs, objects),
		Sets:    data.Migrations(blobfsSet),
	}, nil
}
