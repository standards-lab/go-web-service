package app

import (
	"fmt"

	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"

	dbadmin "github.com/standards-lab/go-web-service/admin/database"
	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/internal/config"
)

// Admin composes the administrative services, one field per admin domain:
// the administrative counterpart of Domain, each service administering one
// infrastructure service over the library mechanisms it triggers.
type Admin struct {
	Database *admin.Service
}

// newAdmin wires the admin layer over infra, each admin service handed its
// switches from cfg at the construction site. It takes lc because an admin
// service owns a lifecycle stage: the database admin service verifies and
// corrects the schema at stage 1, ahead of the domains that verify their
// statements. The content it administers is the data package's: the
// migration set behind the migrator, the seeder, the catalog, and the
// statements registry.
func newAdmin(
	infra *Infrastructure,
	cfg *config.Config,
	lc *lifecycle.Coordinator,
) (*Admin, error) {
	migrator, err := migrate.New(infra.SQL.DB, data.Migrations(), migrate.Options{Logger: infra.Logger})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db := admin.New(infra.DB, infra.SQL.DB, migrator, infra.SQL.Catalog, admin.Options{
		Seed:     cfg.Admin.SeedState(),
		Seeder:   data.NewSeeder(infra.SQL),
		Registry: infra.SQL,
		Logger:   infra.Logger,
	})
	db.Register(lc)
	return &Admin{Database: db}, nil
}

// mountAdmin builds the admin mount, /admin, with each admin domain's route
// group mounted into it. In production the mount belongs on its own
// listener, authenticated and unreachable from the public API's network
// path; that isolation is the v1.data.sql.integration.listener task, and
// until then the mount serves on the API listener.
func mountAdmin(adm *Admin) *web.Group {
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(adm.Database))
	return g
}
