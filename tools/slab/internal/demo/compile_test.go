package demo

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func run(t *testing.T, ctx context.Context) (string, error) {
	t.Helper()
	s, ok := scenario.Lookup("sqlate")
	if !ok {
		t.Fatal("sqlate is not registered")
	}
	var out bytes.Buffer
	err := scenario.Run(ctx, s, scenario.NewReporter(&out, false))
	return out.String(), err
}

func TestScenario_NarratesFourStepsOverTheCheckout(t *testing.T) {
	out, err := run(t, context.Background())
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"[1/4]", "[4/4]",
		"data/patterns/identity.sql",
		"RETURNING id, version",
		"domain/organization/statements/create.sql",
		"VALUES ({{parent_id:uuid}}, {{code}}, {{name}})",
		"{{> app.identity}}",
		`query.NewCatalog(query.Patterns(), query.Publish("app", fsys, "data/patterns"))`,
		`catalog.Compile(fsys, "domain/organization/statements", postgres.Dialect{})`,
		"create, compiled for execution by postgres",
		"VALUES (CAST($1 AS uuid), $2, $3)",
		"$1  parent_id",
		"$2  code",
		"$3  name",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[5/") {
		t.Errorf("the scenario narrates more than four steps:\n%s", out)
	}
	// The compiled SQL block runs from its caption to the next caption; the
	// authored statement and the note under it name the {{ }} markers and
	// are not checked.
	start := strings.Index(out, "create, compiled for execution by postgres")
	end := start + strings.Index(out[start:], "Placeholders, in position order")
	if compiled := out[start:end]; strings.Contains(compiled, "{{") {
		t.Errorf("a {{ marker survived into the compiled text:\n%s", compiled)
	}
}

func TestScenario_StopsAtTheFirstStepWhenTheRepoIsNotARoot(t *testing.T) {
	ctx := env.WithContext(context.Background(), env.Env{Repo: t.TempDir()})
	out, err := run(t, ctx)
	if err == nil {
		t.Fatalf("run succeeded with a --repo that is no repository:\n%s", out)
	}
	if !strings.Contains(out, "[1/4]") || strings.Contains(out, "[2/4]") {
		t.Errorf("run did not stop at step 1:\n%s", out)
	}
}

func TestList_ShowsTheScenarioWithNoNeeds(t *testing.T) {
	var out bytes.Buffer
	root := cli.Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"list"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
	summary := "sqlate    One pattern, one statement that includes it, the two calls that register them, and the compiled result (no compose stack needed)"
	lines := strings.Split(out.String(), "\n")
	at := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, summary) })
	if at < 0 {
		t.Fatalf("list lacks the scenario and its summary:\n%s", out.String())
	}
	// The listing prints a scenario's needs on the lines under its summary,
	// each starting with "needs", so the line after this one belongs to the
	// next scenario when there are none.
	if next := strings.TrimSpace(lines[at+1]); strings.HasPrefix(next, "needs ") {
		t.Errorf("list shows a need for a scenario that has none:\n%s", out.String())
	}
}
