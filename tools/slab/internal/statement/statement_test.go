package statement_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/tools/slab/internal/statement"
)

// compiled builds a two-statement set over an in-memory directory.
func compiled(t *testing.T) *query.Statements {
	t.Helper()
	fsys := fstest.MapFS{
		"statements/create.sql": {Data: []byte("--| tier: standard\nINSERT INTO t (a, b) VALUES ({{a}}, {{b}})\n")},
		"statements/find.sql":   {Data: []byte("--| tier: standard\nSELECT a FROM t WHERE b = {{b}} AND a = {{a}}\n")},
	}
	catalog, err := query.NewCatalog(query.Patterns())
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := catalog.Compile(fsys, "statements", postgres.Dialect{})
	if err != nil {
		t.Fatal(err)
	}
	return stmts
}

func TestFind_ReturnsTheNamedStatement(t *testing.T) {
	st, err := statement.Find(compiled(t), "find")
	if err != nil {
		t.Fatalf("Find(find) = %v", err)
	}
	if st.Name() != "find" || !strings.Contains(st.Text(), "$1") {
		t.Errorf("Find(find) = %q %q", st.Name(), st.Text())
	}
}

func TestFind_NamesAMissingStatement(t *testing.T) {
	_, err := statement.Find(compiled(t), "delete")
	if err == nil || !strings.Contains(err.Error(), `"delete"`) {
		t.Fatalf("Find(delete) = %v, want an error naming the statement", err)
	}
}

func TestPlaceholders_NumbersFromOne(t *testing.T) {
	placeholder := func(i int) string { return fmt.Sprintf("$%d", i) }
	got := statement.Placeholders(placeholder, []string{"b", "a"})
	want := [][2]string{{"$1", "b"}, {"$2", "a"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Placeholders = %v, want %v", got, want)
	}
	if got := statement.Placeholders(placeholder, nil); len(got) != 0 {
		t.Errorf("Placeholders(nil) = %v, want none", got)
	}
}
