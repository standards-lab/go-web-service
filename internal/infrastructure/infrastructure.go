package infrastructure

import (
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-web-service/internal/config"
)

type Infrastructure struct {
	Logger *slog.Logger
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

	return &Infrastructure{
		Logger: logger,
	}, nil
}
