package scenarios

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/repo"
	"github.com/standards-lab/go-web-service/tools/slab/statement"
)

// The sources the sqlate scenario reads, relative to the repository root,
// are one pattern in the application's namespace and the one statement
// that includes it.
const (
	appNamespace  = "app"
	patternsDir   = "data/patterns"
	pattern       = "identity"
	statementsDir = "domain/organization/statements"
	statementName = "create"
)

// sqlate is what the sqlate scenario's steps hand forward: each step fills
// what a later one reads. The scenario is built once, over one sqlate, and
// a run's first step replaces the filesystem and the third the statements
// before any later step reads them, so nothing a run reads is left from a
// run before it.
type sqlate struct {
	n     Nodes
	fsys  fs.FS
	stmts *query.Statements
}

// Sqlate returns the sqlate scenario over n: one pattern, one statement
// that includes it, the two calls that register them, and the compiled
// result. It runs in process over the repository's sources on disk, so it
// needs nothing running; it declares the repo node, which locates the
// repository, and the no color node, which decides its blocks' color.
func Sqlate(n Nodes) scenario.Scenario {
	s := &sqlate{n: n}
	return scenario.Scenario{
		Name:    "sqlate",
		Summary: "One pattern, one statement that includes it, the two calls that register them, and the compiled result (no compose stack needed)",
		Nodes:   []graph.Ref{n.Repo, n.NoColor},
		Steps: []scenario.Step{
			{Intent: "Write a pattern", Action: s.writePattern},
			{Intent: "Write a statement", Action: s.writeStatement},
			{Intent: "Catalog registration", Action: s.registerCatalog},
			{Intent: "Show the compiled output", Action: s.showCompiled},
		},
	}
}

func (s *sqlate) writePattern(ctx context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	// repo.FS reads the --repo override from the environment on ctx.
	fsys, err := repo.FS(env.WithContext(ctx, env.Env{Repo: inv.Get(s.n.Repo)}))
	if err != nil {
		return err
	}
	s.fsys = fsys
	file := path.Join(patternsDir, pattern+".sql")
	text, err := fs.ReadFile(s.fsys, file)
	if err != nil {
		return err
	}
	st := styleOf(s.n, inv)
	if err := show(r, file, output.SQL(st, string(text))); err != nil {
		return err
	}
	if err := show(r, "An embed directive over an embed.FS variable", output.SQL(st, "//go:embed patterns/*.sql\nvar patterns embed.FS")); err != nil {
		return err
	}
	r.Note("The SQL pattern files are embedded into the binary using an embed directive, //go:embed patterns/*.sql. The embedded files can be accessed via an embed.FS variable.")
	return nil
}

func (s *sqlate) writeStatement(_ context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	file := path.Join(statementsDir, statementName+".sql")
	text, err := fs.ReadFile(s.fsys, file)
	if err != nil {
		return err
	}
	st := styleOf(s.n, inv)
	if err := show(r, file, output.SQL(st, string(text))); err != nil {
		return err
	}
	r.Note("The statement reuses the %s pattern above, and declares three parameters of its own: {{parent_id:uuid}}, {{code}}, {{name}}.", pattern)
	if err := show(r, "An embed directive over an embed.FS variable", output.SQL(st, "//go:embed statements/*.sql\nvar statements embed.FS")); err != nil {
		return err
	}
	r.Note("The SQL statements are embedded in the same way as the patterns, sourced from statements/*.sql.")
	return nil
}

func (s *sqlate) registerCatalog(_ context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	st := styleOf(s.n, inv)
	if err := show(r, "fsys, the filesystem both calls below read from", output.SQL(st, "fsys, err := repo.FS(ctx)")); err != nil {
		return err
	}
	r.Note("repo.FS(ctx) resolves the repository root — the --repo flag when it is set, or the nearest ancestor directory whose go.mod declares this module — and returns an os.DirFS rooted there. Every path this scenario reads, the pattern and statement above included, is relative to that root.")
	if err := show(r, fmt.Sprintf("The pattern registered under the %q namespace, beside blobfs's published namespace", appNamespace),
		output.SQL(st, fmt.Sprintf("query.NewCatalog(query.Patterns(), bfdata.Patterns(), query.Publish(%q, fsys, %q))", appNamespace, patternsDir))); err != nil {
		return err
	}
	catalog, err := query.NewCatalog(query.Patterns(), bfdata.Patterns(), query.Publish(appNamespace, s.fsys, patternsDir))
	if err != nil {
		return err
	}
	if err := show(r, "A directory of statements, compiled against the postgres dialect",
		output.SQL(st, fmt.Sprintf("catalog.Compile(fsys, %q, postgres.Dialect{})", statementsDir))); err != nil {
		return err
	}
	stmts, err := catalog.Compile(s.fsys, statementsDir, postgres.Dialect{})
	if err != nil {
		return err
	}
	s.stmts = stmts
	r.Note("fsys and the directory path indicate where the .sql files are sourced from. You can register multiple pattern and statement sources. Patterns are encapsulated under their specified namespace: a library that ships statements publishes its own, as blobfs does, and the organization's logo statements include its file columns.")
	return nil
}

func (s *sqlate) showCompiled(_ context.Context, inv *cli.Invocation, r *scenario.Reporter) error {
	stmt, err := statement.Find(s.stmts, statementName)
	if err != nil {
		return fmt.Errorf("%w from %s", err, statementsDir)
	}
	text := stmt.Text()
	if strings.Contains(text, "{{") {
		return fmt.Errorf("%s: a {{ marker survived compilation:\n%s", statementName, text)
	}
	st := styleOf(s.n, inv)
	if err := show(r, statementName+", compiled for execution by postgres", output.SQL(st, text)); err != nil {
		return err
	}
	if err := show(r, "Placeholders, in position order", output.Table(st, statement.Placeholders(postgres.Dialect{}.Placeholder, stmt.Params()))); err != nil {
		return err
	}
	r.Note("Compile splices the included pattern and rewrites each parameter to postgres's $n placeholder, numbered in first-occurrence order. The :uuid on parent_id became the CAST around its placeholder.")
	return nil
}
