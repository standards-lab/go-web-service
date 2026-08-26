package sdk_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/query"
	"github.com/standards-lab/go-web-service/sdk"
)

// dialect is the minimal test dialect: positional placeholders, no error
// mapping. Rendering tests need a dialect, not an engine.
type dialect struct{}

func (dialect) Name() string             { return "test" }
func (dialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (dialect) MapError(err error) error { return err }

var _ database.Dialect = dialect{}

func render(t *testing.T, s query.Select) string {
	t.Helper()
	text, _, err := s.SQL(dialect{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return text
}

func TestColumns_PrefixesNamesAndAppendsExtra(t *testing.T) {
	got := render(t, query.Select{
		Columns: sdk.Columns("t", []string{"a", "b"},
			query.Column{Expr: query.Raw("'x'")},
		),
		From: query.Table("t"),
	})
	if got != "SELECT t.a, t.b, 'x' FROM t" {
		t.Errorf("rendered %q", got)
	}
}

func TestColumns_EmptyPrefixRendersBareNames(t *testing.T) {
	got := render(t, query.Select{
		Columns: sdk.Columns("", []string{"a"}),
		From:    query.Table("t"),
	})
	if got != "SELECT a FROM t" {
		t.Errorf("rendered %q", got)
	}
}

func TestFields_NamesAreTheContract(t *testing.T) {
	p := query.Projection{
		From:   query.Table("t"),
		Key:    query.Field{Name: "id", Expr: query.Col("id")},
		Fields: sdk.Fields([]string{"a", "b"}),
	}
	stmts, err := p.Statements(dialect{}, query.Directives{Page: query.Page{Number: 1, Size: 10}})
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	for _, want := range []string{"id AS id", "a AS a", "b AS b"} {
		if !strings.Contains(stmts.Page.SQL, want) {
			t.Errorf("page SQL %q missing %q", stmts.Page.SQL, want)
		}
	}
}

func TestRecursivePath_RootedProjection(t *testing.T) {
	p := sdk.RecursivePath{
		Table:     "org",
		Key:       "id",
		Parent:    "parent_id",
		Segment:   "code",
		Field:     "path",
		Separator: "/",
		Rooted:    true,
		Columns:   []string{"id", "code"},
	}.Projection()

	stmts, err := p.Statements(dialect{}, query.Directives{Page: query.Page{Number: 1, Size: 10}})
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}

	// The pattern's whole shape, pinned: the recursive CTE re-presenting
	// the table with path, the rooted anchor, the step appending each
	// child's segment, and the projection reading from the CTE. The count
	// carries the same CTE.
	for _, stmt := range []string{stmts.Page.SQL, stmts.Count.SQL} {
		for _, want := range []string{
			"WITH RECURSIVE org_path (id, code, path) AS",
			"'/' || o.code",
			"WHERE o.parent_id IS NULL",
			"l.path || '/' || o.code",
			"JOIN org_path l ON o.parent_id = l.id",
			"FROM org_path",
		} {
			if !strings.Contains(stmt, want) {
				t.Errorf("SQL %q missing %q", stmt, want)
			}
		}
	}
	// path is an ordinary projected field: sortable like any other.
	if _, err := p.Statements(dialect{}, query.Directives{
		Page: query.Page{Number: 1, Size: 10},
		Sort: []query.Sort{{Field: "path"}},
	}); err != nil {
		t.Errorf("sort by path: %v", err)
	}
}

func TestRecursivePath_UnrootedJoinsWithoutPrefix(t *testing.T) {
	p := sdk.RecursivePath{
		Table:     "goal",
		Key:       "id",
		Parent:    "parent_id",
		Segment:   "slug",
		Field:     "path",
		Separator: ".",
		Columns:   []string{"id", "slug"},
	}.Projection()

	stmts, err := p.Statements(dialect{}, query.Directives{Page: query.Page{Number: 1, Size: 10}})
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	if !strings.Contains(stmts.Page.SQL, "l.path || '.' || o.slug") {
		t.Errorf("step missing dotted join: %q", stmts.Page.SQL)
	}
	// An unrooted anchor is the bare segment, no leading separator.
	if strings.Contains(stmts.Page.SQL, "'.' || o.slug, ") || strings.Contains(stmts.Page.SQL, "AS (SELECT o.id, o.slug, '.'") {
		t.Errorf("anchor unexpectedly rooted: %q", stmts.Page.SQL)
	}
}

func TestRecursivePath_PanicsWithoutSeparator(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Projection with no separator did not panic")
		}
	}()
	_ = sdk.RecursivePath{Table: "t", Key: "id"}.Projection()
}
