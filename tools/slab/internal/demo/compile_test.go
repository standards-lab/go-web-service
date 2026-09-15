package demo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

func run(t *testing.T, ctx context.Context) (string, error) {
	t.Helper()
	s, ok := scenario.Lookup("sqlate:compile")
	if !ok {
		t.Fatal("sqlate:compile is not registered")
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
		"data/patterns/identity.sql, as authored",
		"RETURNING id, version",
		"domain/organization/statements/create.sql, as authored",
		"VALUES ({{parent_id:uuid}}, {{code}}, {{name}})",
		"{{> app.identity}}",
		`query.NewCatalog(query.Patterns(), query.Publish("app", fsys, "data/patterns"))`,
		`catalog.Compile(fsys, "domain/organization/statements", postgres.Dialect{})`,
		"create, as postgres receives it",
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
	// notes after it mention the {{> }} marker by name and are not checked.
	start := strings.Index(out, "create, as postgres receives it")
	end := start + strings.Index(out[start:], "Placeholders, in position order")
	if compiled := out[start:end]; strings.Contains(compiled, "{{") {
		t.Errorf("a {{ marker survived into the compiled text:\n%s", compiled)
	}
}

func TestScenario_StopsAtTheFirstStepWhenTheRepoIsNotARoot(t *testing.T) {
	ctx := scenario.WithEnv(context.Background(), scenario.Env{Repo: t.TempDir()})
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
	if !strings.Contains(out.String(), "sqlate:compile  One pattern, one statement that includes it, the two calls that register them, and the compiled result (no compose stack needed)") {
		t.Errorf("list lacks the scenario and its summary:\n%s", out.String())
	}
	if strings.Contains(out.String(), "needs ") {
		t.Errorf("list shows a need for a scenario that has none:\n%s", out.String())
	}
}
