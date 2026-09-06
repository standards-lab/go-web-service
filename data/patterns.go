package data

import (
	"embed"

	"github.com/standards-lab/sqlate/query"
)

//go:embed patterns/*.sql
var patternFiles embed.FS

// Namespace is the application's pattern namespace: a statement includes
// one of its patterns as {{> app.name}}.
const Namespace = "app"

// Patterns is the application's pattern namespace, the protocol SQL its
// domains share. A pattern is a property of the application, so a domain
// that needs another's has found application content, and it lives here
// beside the migrations and seeds. The composition root registers it in
// the catalog alongside the library's.
func Patterns() query.Source {
	return query.Publish(Namespace, patternFiles, "patterns")
}
