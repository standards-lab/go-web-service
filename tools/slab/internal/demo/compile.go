// Package demo holds the scenarios that narrate a library's mechanism in
// process, over the service's own sources on disk, with nothing running.
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
		Name:    "sqlate:compile",
		Summary: "One pattern, one statement that includes it, the two calls that register them, and the compiled result (no compose stack needed)",
		Steps: []scenario.Step{
			{Intent: "Write a pattern", Action: s.writePattern},
			{Intent: "Write a statement that includes it, with parameters", Action: s.writeStatement},
			{Intent: "Register both, one line each", Action: s.register},
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
	r.SQL(file+", as authored", string(text))
	r.Note("This is the application's own pattern namespace, %s: protocol SQL authored once and included", appNamespace)
	r.Note("by name from any statement that needs it, the same way the library's own patterns work.")
	return nil
}

func (s *state) writeStatement(_ context.Context, r *scenario.Reporter) error {
	file := path.Join(statementsDir, statement+".sql")
	text, err := fs.ReadFile(s.fsys, file)
	if err != nil {
		return err
	}
	r.SQL(file+", as authored", string(text))
	r.Note("It includes exactly one pattern, {{> %s.%s}}, and declares three parameters of its own:", appNamespace, pattern)
	r.Note("{{parent_id:uuid}}, {{code}}, and {{name}}.")
	return nil
}

func (s *state) register(_ context.Context, r *scenario.Reporter) error {
	r.SQL("The pattern namespace, registered in one call",
		fmt.Sprintf("query.NewCatalog(query.Patterns(), query.Publish(%q, fsys, %q))", appNamespace, patternsDir))
	catalog, err := query.NewCatalog(query.Patterns(), query.Publish(appNamespace, s.fsys, patternsDir))
	if err != nil {
		return err
	}
	r.SQL("The statements directory, compiled against a dialect in one call",
		fmt.Sprintf("catalog.Compile(fsys, %q, postgres.Dialect{})", statementsDir))
	stmts, err := catalog.Compile(s.fsys, statementsDir, postgres.Dialect{})
	if err != nil {
		return err
	}
	s.stmts = stmts
	r.Note("Publish names the directory a namespace's patterns are read from; NewCatalog reads them beside the")
	r.Note("library's own. Compile reads every .sql file in the directory and resolves each against that catalog.")
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
	r.SQL(statement+", as postgres receives it", text)
	r.Table("Placeholders, in position order", placeholderRows(postgres.Dialect{}.Placeholder, st.Params()))
	r.Note("Compile splices the include in: RETURNING id, version now sits in the text, and no {{> }} marker remains.")
	r.Note("Compile also rewrites each parameter to postgres's $n placeholder, numbered in first-occurrence order;")
	r.Note("the :uuid on parent_id became the CAST around its placeholder.")
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
