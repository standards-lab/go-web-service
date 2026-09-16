// Package demo holds the narrated scenarios: sqlate, which runs a library's
// mechanism in process over the service's own sources on disk with nothing
// running, and domain, which runs the organization domain's full CRUD
// surface against the running service, reseeded from a known fixture each
// run.
package demo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/tools/slab/internal/repo"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// The sources the scenario reads, relative to the repository root, are one
// pattern in the application's namespace and the one statement that
// includes it.
const (
	appNamespace  = "app"
	patternsDir   = "data/patterns"
	pattern       = "identity"
	statementsDir = "domain/organization/statements"
	statement     = "create"
)

func init() {
	scenario.Add(compileScenario())
}

// state is what the steps hand forward: each step fills what a later one
// reads. A scenario is built over one state, so its steps run in order over
// the same filesystem and statements.
type state struct {
	fsys  fs.FS
	stmts *query.Statements
}

func compileScenario() scenario.Scenario {
	s := &state{}
	return scenario.Scenario{
		Name:    "sqlate",
		Summary: "One pattern, one statement that includes it, the two calls that register them, and the compiled result (no compose stack needed)",
		Steps: []scenario.Step{
			{Intent: "Write a pattern", Action: s.writePattern},
			{Intent: "Write a statement", Action: s.writeStatement},
			{Intent: "Catalog registration", Action: s.register},
			{Intent: "Show the compiled output", Action: s.showCompiled},
		},
	}
}

func (s *state) writePattern(ctx context.Context, r *scenario.Reporter) error {
	root, err := repo.Root(ctx)
	if err != nil {
		return err
	}
	s.fsys = os.DirFS(root)
	file := path.Join(patternsDir, pattern+".sql")
	text, err := fs.ReadFile(s.fsys, file)
	if err != nil {
		return err
	}
	r.SQL(file, string(text))
	return nil
}

func (s *state) writeStatement(_ context.Context, r *scenario.Reporter) error {
	file := path.Join(statementsDir, statement+".sql")
	text, err := fs.ReadFile(s.fsys, file)
	if err != nil {
		return err
	}
	r.SQL(file, string(text))
	r.Note("The statement reuses the %s pattern above, and declares three parameters of its own: {{parent_id:uuid}}, {{code}}, {{name}}.", pattern)
	return nil
}

func (s *state) register(_ context.Context, r *scenario.Reporter) error {
	r.SQL(fmt.Sprintf("The pattern registered under the %q namespace", appNamespace),
		fmt.Sprintf("query.NewCatalog(query.Patterns(), query.Publish(%q, fsys, %q))", appNamespace, patternsDir))
	catalog, err := query.NewCatalog(query.Patterns(), query.Publish(appNamespace, s.fsys, patternsDir))
	if err != nil {
		return err
	}
	r.SQL("A directory of statements, compiled against the postgres dialect",
		fmt.Sprintf("catalog.Compile(fsys, %q, postgres.Dialect{})", statementsDir))
	stmts, err := catalog.Compile(s.fsys, statementsDir, postgres.Dialect{})
	if err != nil {
		return err
	}
	s.stmts = stmts
	r.Note("fsys and the directory path indicate where the .sql files are sourced from. You can register multiple pattern and statement sources. Patterns are encapsulated under their specified namespace.")
	return nil
}

func (s *state) showCompiled(_ context.Context, r *scenario.Reporter) error {
	st, err := find(s.stmts, statement)
	if err != nil {
		return err
	}
	text := st.Text()
	if strings.Contains(text, "{{") {
		return fmt.Errorf("%s: a {{ marker survived compilation:\n%s", statement, text)
	}
	r.SQL(statement+", compiled for execution by postgres", text)
	r.Table("Placeholders, in position order", placeholderRows(postgres.Dialect{}.Placeholder, st.Params()))
	r.Note("Compile splices the included pattern and rewrites each parameter to postgres's $n placeholder, numbered in first-occurrence order. The :uuid on parent_id became the CAST around its placeholder.")
	return nil
}

// placeholderRows maps each parameter to the placeholder its position renders.
func placeholderRows(placeholder func(int) string, names []string) [][2]string {
	rows := make([][2]string, len(names))
	for i, name := range names {
		rows[i] = [2]string{placeholder(i + 1), name}
	}
	return rows
}

// find returns the statement named name from stmts, as an error rather than
// the panic Statements.Statement reserves for a constructor's defect.
func find(stmts *query.Statements, name string) (query.Statement, error) {
	for _, st := range stmts.Statements() {
		if st.Name() == name {
			return st, nil
		}
	}
	return query.Statement{}, fmt.Errorf("no statement %q compiled from %s", name, statementsDir)
}
