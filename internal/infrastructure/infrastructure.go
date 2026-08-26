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
// shutdown, or readiness declaration: the database registers at stage 0 with
// its readiness check, ahead of the root stage. Construction performs no I/O
// — connectivity belongs to the database's Start. A nil lc skips every
// registration, which is how cmd/db reuses the construction and drives the
// database's lifecycle itself.
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
