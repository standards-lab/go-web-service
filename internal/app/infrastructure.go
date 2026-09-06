package app

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/sqlate"
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
// grouped with the pattern catalog every statement compiles against. The
// struct stops at the composition root: the layer files read its fields,
// and a package receives its dependencies as constructor parameters, never
// the struct itself.
type Infrastructure struct {
	Logger *slog.Logger
	DB     *database.DB
	SQL    *data.Database
}

// newInfrastructure constructs the infrastructure services in one place, in
// dependency order, each registering on lc where it is built as a
// lifecycle.Service with the stage that places it in the startup order, so
// a service cannot exist without a startup, shutdown, or readiness
// declaration. The database registers at stage 0 with its readiness check.
// Construction opens nothing: connectivity belongs to a service's Start.
// The pattern catalog is built here, once: the library's namespace and the
// application's; a port adds the engine's overlay beside them.
func newInfrastructure(
	w io.Writer,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Infrastructure, error) {
	logger := logging.New(w, cfg.Log)

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

	catalog := query.MustCatalog(query.Patterns(), data.Patterns())

	return &Infrastructure{
		Logger: logger,
		DB:     db,
		SQL:    data.New(sqlate.Wrap(db.Conn(), pgdialect.Dialect{}), catalog),
	}, nil
}
