package data

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is the embedded schema set, NNNN_name.{up,down}.sql under
// migrations/. A layout defect is a wiring defect and panics at cold start.
func Migrations() []migrate.Migration {
	ms, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("data: %v", err))
	}
	return ms
}
