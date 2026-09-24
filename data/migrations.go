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

// Migrations is the service's migration sets in declaration order, the
// last the service's own: NNNN_name.{up,down}.sql under migrations/. A
// layout defect is a wiring defect and panics at cold start.
func Migrations() []migrate.Set {
	ms, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("data: %v", err))
	}
	return []migrate.Set{{Name: AppSet, Table: "schema_version", Migrations: ms}}
}
