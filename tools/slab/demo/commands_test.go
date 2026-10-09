package demo_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/demo"
	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func noEnv() env.Env { return env.Env{} }

func plainReporter(w io.Writer) *scenario.Reporter { return scenario.NewReporter(w, false) }

func TestScenarios_ListsEachOnceInPresentationOrder(t *testing.T) {
	var names []string
	for _, s := range demo.Scenarios() {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "sqlate,domain,problems,storage" {
		t.Errorf("Scenarios() = %s; want sqlate,domain,problems,storage", got)
	}
}

// cobra sorts a command's subcommands by name, so the test checks the set
// and not the order; the order Scenarios returns is the listing's concern.
func TestCommands_MountsOneSubcommandPerScenario(t *testing.T) {
	cmd := demo.Commands(noEnv, plainReporter)
	if cmd.Name() != "demo" {
		t.Errorf("Commands().Name() = %q; want demo", cmd.Name())
	}
	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	if got := strings.Join(names, ","); got != "domain,problems,sqlate,storage" {
		t.Errorf("Commands() subcommands = %s; want domain,problems,sqlate,storage", got)
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
