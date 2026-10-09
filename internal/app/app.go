package app

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/domain/document"
	"github.com/standards-lab/go-web-service/domain/organization"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/sdk"
)

// App is the application layer: the dependency graph that describes the
// service, the [Nodes] that name its parts, and the configuration and log
// writer it was described over.
type App struct {
	graph *graph.Graph
	nodes Nodes
	cfg   *config.Config
	w     io.Writer
	ran   bool
}

// Nodes is the App's graph, one handle per node: the single description of
// what the service is composed of. Each layer file's define function fills
// its own part of one Nodes value, and its constructors read the lower
// layers' nodes from that same value. Each node's name, in the field's
// comment, is the name the lifecycle labels its errors with and the
// readiness probe reports its check under. A value only one node uses is
// built inside that node's constructor rather than as a node of its own.
type Nodes struct {
	// Infrastructure (infrastructure.go).
	Config   *graph.Node[*config.Config] // "config"
	Logger   *graph.Node[*slog.Logger]   // "logger"
	Database *graph.Node[*database.DB]   // "database"
	Storage  *graph.Node[*storage.Store] // "storage"
	SQL      *graph.Node[*data.Database] // "sql"
	Files    *graph.Node[*data.Storage]  // "files"

	// Telemetry (telemetry.go).
	Telemetry *graph.Node[*Telemetry] // "telemetry"

	// Admin (admin.go).
	Gate   *graph.Node[*sdk.Gate]      // "gate"
	Schema *graph.Node[*admin.Service] // "schema"

	// Domain (domain.go).
	Organization *graph.Node[*organization.Service] // "organization"
	Document     *graph.Node[*document.Service]     // "document"

	// Reactors (reactors.go).
	Wake    *graph.Node[*Wake]                   // "wake"
	Sweeper *graph.Node[*sdk.Reactor[time.Time]] // "sweeper"

	// The request edge (server.go).
	Readiness *graph.Node[*lifecycle.Readiness] // "readiness"
	Router    *graph.Node[*web.Router]          // "router"
	Server    *graph.Node[*web.Server]          // "server"

	// Reactors are the reactor layer's Build roots: a reactor is reached by
	// no Use, so Run builds each as a root, and the server orders itself
	// after each so it stays the top layer.
	Reactors []graph.Ref
}

// New describes the graph over cfg and a writer for the service's logger,
// one layer file at a time, lowest first, and returns the App. It is cold:
// it constructs nothing and cannot fail; [App.Run] builds.
func New(cfg *config.Config, w io.Writer) *App {
	a := &App{graph: graph.New(), cfg: cfg, w: w}
	defineInfrastructure(a.graph, &a.nodes, cfg, w)
	defineTelemetry(a.graph, &a.nodes)
	defineAdmin(a.graph, &a.nodes)
	defineDomain(a.graph, &a.nodes)
	defineReactors(a.graph, &a.nodes)
	defineServer(a.graph, &a.nodes)
	return a
}

// Graph returns the graph Run builds from, as [New] described it. The
// service itself never calls it; it is published so a caller can, before
// Run, observe what the Build constructs with graph.Graph.Observe, or
// Replace a node's constructor with a substitute.
func (a *App) Graph() *graph.Graph { return a.graph }

// Nodes returns a handle on each of a's graph nodes, for a caller's Replace
// or Observe.
func (a *App) Nodes() Nodes { return a.nodes }

// Run builds the graph, hands the System to a lifecycle Coordinator with
// the config node's lifecycle block, and runs it until ctx ends, returning
// the process exit code. The Build's roots are every node Run reads (the
// config, the logger, the server), the schema, and the reactors. Once the
// server has bound, the ready record names its address. Run logs "server
// stopped" and returns 0 after a clean drain, or logs "service failed" with
// the error and returns 1 on a Build, startup, runtime, or shutdown
// failure. An App runs once: a second Run panics before it builds anything.
func (a *App) Run(ctx context.Context) int {
	if a.ran {
		panic("app: Run called twice; an App runs once")
	}
	a.ran = true

	n := a.nodes
	roots := append([]graph.Ref{n.Config, n.Logger, n.Server, n.Schema}, n.Reactors...)
	sys, err := a.graph.Build(roots...)
	if err != nil {
		// The logger node may be what failed, so the Build failure is
		// reported through a logger of Run's own over the same writer and
		// log configuration.
		logging.New(a.w, a.cfg.Log).Error("service failed", "error", err)
		return 1
	}

	logger := sys.Get(n.Logger)
	server := sys.Get(n.Server)
	lc := lifecycle.New(sys, sys.Get(n.Config).Config)
	lc.OnReady(func() {
		logger.Info("server ready", "addr", server.Addr())
	})

	if err := lc.Run(ctx); err != nil {
		logger.Error("service failed", "error", err)
		return 1
	}
	logger.Info("server stopped")
	return 0
}
