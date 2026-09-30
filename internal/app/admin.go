package app

import (
	"fmt"
	"log/slog"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"

	dbadmin "github.com/standards-lab/go-web-service/admin/database"
	storageadmin "github.com/standards-lab/go-web-service/admin/storage"
	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/sdk"
)

// Admin composes the administrative services, one field per admin domain:
// the administrative counterpart of Domain, each service administering one
// infrastructure service over the library mechanisms it triggers. Gate is
// the process's quiesce gate, which the database admin domain holds
// exclusively around every verb that changes the schema, so the sweep's
// passes, which hold it shared, never run under one.
type Admin struct {
	Database *admin.Service
	Storage  *storage.Store
	Gate     *sdk.Gate
}

// newAdmin wires the admin layer over infra, each admin service handed its
// switches from cfg. It declares the database admin service on lc at
// stageSchema, with the service as its own readiness check. The service
// administers the data package's content: the migration sets, the
// catalog, the statements registry, and the seeder, composed from dom's
// seed contributions in the tables' dependency order (the organizations'
// rows, then the logos and document trees that name them). gate is the
// quiesce gate the database admin routes hold around a schema change;
// startup's own correction takes none, since it runs before the sweep's
// stage starts.
func newAdmin(
	infra *Infrastructure,
	dom *Domain,
	cfg *config.Config,
	gate *sdk.Gate,
	lc *lifecycle.Coordinator,
) (*Admin, error) {
	migrator, err := migrate.New(infra.SQL.DB, infra.Sets, migrate.Options{Logger: infra.Logger})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db := admin.New(infra.DB, infra.SQL.DB, migrator, infra.SQL.Catalog, admin.Options{
		Seed:     cfg.Admin.SeedState(),
		Seeder:   data.NewSeeder(infra.SQL, dom.Organization.Seed(), dom.Organization.LogoSeed(), dom.Document.Seed()),
		Registry: infra.SQL,
		Logger:   infra.Logger,
	})
	lc.Add(lifecycle.Service{
		Name:  "schema",
		Stage: stageSchema,
		Start: db.Start,
		Check: db,
	})
	return &Admin{Database: db, Storage: infra.ObjectStore, Gate: gate}, nil
}

// mountAdmin builds the admin mount, /admin, with each admin domain's route
// group mounted into it. In production the mount belongs on its own
// listener, authenticated and unreachable from the public API's network
// path. Until the planned management listener exists, the mount serves on
// the API listener. Each group's error writer logs through logger.
func mountAdmin(adm *Admin, logger *slog.Logger) *web.Group {
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(adm.Database, adm.Gate, logger))
	g.Mount(storageadmin.Routes(adm.Storage, logger))
	return g
}
