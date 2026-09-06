package app

import (
	"context"
	"io"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/internal/config"
)

// App is the application layer: it assembles infrastructure, the admin
// layer, the domain, and the reactors into a router and a lifecycle
// coordinator, and runs the process.
type App struct {
	cfg    *config.Config
	logger *slog.Logger
	lc     *lifecycle.Coordinator
	server *web.Server
}

// New is the cold start: it composes the layers in dependency order and
// performs no I/O, so a failed construction leaks no connections.
func New(cfg *config.Config, w io.Writer) (*App, error) {
	lc := lifecycle.New()

	infra, err := newInfrastructure(w, cfg, lc)
	if err != nil {
		return nil, err
	}

	adm, err := newAdmin(infra, cfg, lc)
	if err != nil {
		return nil, err
	}

	dom := newDomain(infra, lc)

	if _, err := newReactors(infra, dom, lc); err != nil {
		return nil, err
	}

	router := web.NewRouter()
	router.Use(middleware(infra)...)
	for _, m := range routes(dom, adm, cfg) {
		router.Mount(m)
	}

	server := web.NewServer(cfg.Server, router)
	lc.Add(lifecycle.Service{
		Name:     "server",
		Stage:    lifecycle.StageRoot,
		Start:    server.Start,
		Shutdown: server.Shutdown,
	})
	lc.Monitor(server.Err())

	web.RegisterHealth(router, lc)

	lc.OnReady(func() {
		infra.Logger.Info("server ready", "addr", server.Addr())
	})

	return &App{
		cfg:    cfg,
		logger: infra.Logger,
		lc:     lc,
		server: server,
	}, nil
}

// Run is the hot start plus shutdown: the coordinator starts every stage,
// serves until ctx ends or a service fails, and drains within the
// configured timeout. It returns the process exit code.
func (a *App) Run(ctx context.Context) int {
	if err := a.lc.Run(ctx, a.cfg.ShutdownTimeout.Duration()); err != nil {
		a.logger.Error("service failed", "error", err)
		return 1
	}
	a.logger.Info("server stopped")
	return 0
}
