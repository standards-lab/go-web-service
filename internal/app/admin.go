package app

import (
	"fmt"

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
// switches from cfg at the construction site. It takes lc because an admin
// service owns a lifecycle stage: the database admin service verifies and
// corrects the schema at stageSchema, ahead of the statements verified at
// stageVerify. The root declares that service itself, as go-database's
// Register would (the name, the Start, and the service as its readiness
// check), so its stage is named at the call site from the stage table
// rather than taken inside the library. The content it administers is the
// data package's: the migration sets behind the migrator, the seeder, the
// catalog, and the statements registry. The seeder composes the domains'
// seed contributions from dom, in the tables' dependency order, so the
// data package reads the states without naming a domain's table: the
// organizations' rows, applied in the seed's transaction, then the stored
// files that name them, the logos and the document trees, written after
// it commits. gate is
// the process's quiesce gate, which the database admin domain's routes
// hold around a schema change. Startup's own schema correction takes no
// gate: it runs at stageSchema, before the sweep's stage starts.
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
// path; that isolation is the v1.admin-listener goal, and until then the
// mount serves on the API listener.
func mountAdmin(adm *Admin) *web.Group {
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(adm.Database, adm.Gate))
	g.Mount(storageadmin.Routes(adm.Storage))
	return g
}
