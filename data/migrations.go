package data

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// AppSet names the service's own migration set, whose history keeps the
// schema_version table it has always used.
const AppSet = "app"

// Migrations is the service's migration sets in declaration order: the
// sets the libraries beneath the service ship, as the composition root
// passes them, then the service's own, NNNN_name.{up,down}.sql under
// migrations/, whose migrations may reference the tables beneath. A layout
// defect is a wiring defect and panics at cold start.
func Migrations(beneath ...migrate.Set) []migrate.Set {
	ms, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("data: %v", err))
	}
	return append(beneath, migrate.Set{Name: AppSet, Table: "schema_version", Migrations: ms})
}
