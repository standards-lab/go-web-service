package infrastructure

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-web-service/internal/config"
)

type Infrastructure struct {
	Logger *slog.Logger
	DB     *database.DB
}

// New constructs the infrastructure services in one place, each registering
// on lc where it's built, so a service can't exist without a startup,
// shutdown, or readiness declaration. lc goes unused today, because the
// template's one service, Logger, has no lifecycle; it stays a parameter so
// the first service that needs one — a database pool, for instance —
// registers here without a signature change.
func New(
	w io.Writer,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Infrastructure, error) {
	logger := logging.New(w, cfg.Log)

	db, err := postgres.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	if lc != nil {
		lc.Add(lifecycle.Service{
			Name:     "database",
			Stage:    0,
			Start:    db.Start,
			Shutdown: db.Shutdown,
			Check:    db,
		})
	}

	return &Infrastructure{
		Logger: logger,
		DB:     db,
	}, nil
}
