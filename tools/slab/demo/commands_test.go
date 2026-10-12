package demo_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/demo"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func TestScenarios_ListsEachOnceInPresentationOrder(t *testing.T) {
	var names []string
	for _, s := range demo.Scenarios() {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "sqlate,domain,problems,storage" {
		t.Errorf("Scenarios() = %s; want sqlate,domain,problems,storage", got)
	}
}

// The demo command's help lists one subcommand per scenario, each with its
// summary, in presentation order. Help builds nothing, so the node the
// Source names is never constructed.
func TestCommands_MountsOneSubcommandPerScenario(t *testing.T) {
	g := graph.New()
	node := g.Define("config", func(*graph.Scope) (struct{}, error) { return struct{}{}, nil })
	lc := g.Define("lifecycle config", func(*graph.Scope) (lifecycle.Config, error) {
		var c lifecycle.Config
		return c, c.Finalize("")
	})
	root := (&cli.Command{Name: "slab"}).Add(demo.Commands(scenario.Source{
		Node:     node,
		Env:      func(*cli.Invocation) env.Env { return env.Env{} },
		Reporter: func(inv *cli.Invocation) *scenario.Reporter { return scenario.NewReporter(inv.Stdout, false) },
	}))
	var out, errOut bytes.Buffer

	code := cli.Run(context.Background(), root, []string{"demo", "--help"}, cli.Streams{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut}, cli.WithGraph(g, lc))

	if code != process.ExitUsage {
		t.Errorf("demo --help exited %d, want %d; stderr = %q", code, process.ExitUsage, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "Run one scenario\n\nUsage:\n  slab demo <command> [flags]\n") {
		t.Errorf("demo help does not open with its summary and usage:\n%s", out.String())
	}
	var names []string
	for _, s := range demo.Scenarios() {
		if !strings.Contains(out.String(), "  "+s.Name+" ") || !strings.Contains(out.String(), s.Summary) {
			t.Errorf("demo help lacks %s with its summary:\n%s", s.Name, out.String())
		}
		names = append(names, s.Name)
	}
	at := -1
	for _, name := range names {
		i := strings.Index(out.String(), "\n  "+name+" ")
		if i < at {
			t.Errorf("demo help lists %s out of presentation order %q:\n%s", name, names, out.String())
		}
		at = i
	}
}

// Each scenario lists its needs on the lines under its summary, the
// preconditions the README names: sqlate none, domain and problems the full
// compose stack, storage postgres, azurite, and the service but no Grafana.
func TestScenarios_ListEachWithItsNeeds(t *testing.T) {
	var out bytes.Buffer
	scenario.WriteListing(&out, demo.Scenarios())
	fullStack := []string{
		"needs postgres (mise run db:up)",
		"needs the observability profile (mise run otel:up)",
		"needs the service (mise run serve)",
		"needs Grafana (mise run otel:up)",
	}
	for name, want := range map[string][]string{
		"sqlate":   nil,
		"domain":   fullStack,
		"problems": fullStack,
		"storage": {
			"needs postgres and azurite (mise run db:up)",
			"needs the service (mise run serve)",
		},
	} {
		if got := needs(t, out.String(), name); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s lists needs %q, want %q", name, got, want)
		}
	}
}

// needs returns the need lines the listing prints under the scenario name:
// every line after its own that has no name in the name column.
func needs(t *testing.T, listing, name string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(listing, "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "  "+name+" ") {
			continue
		}
		var out []string
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "   ") {
				break
			}
			out = append(out, strings.TrimSpace(next))
		}
		return out
	}
	t.Fatalf("the listing lacks %s:\n%s", name, listing)
	return nil
}
