package demo

import (
	"io"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func noEnv() env.Env { return env.Env{} }

func plainReporter(w io.Writer) *scenario.Reporter { return scenario.NewReporter(w, false) }

func TestScenarios_ListsEachOnceInPresentationOrder(t *testing.T) {
	var names []string
	for _, s := range Scenarios() {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "sqlate,domain,problems" {
		t.Errorf("Scenarios() = %s; want sqlate,domain,problems", got)
	}
}

// cobra sorts a command's subcommands by name, so the test checks the set
// and not the order; the order Scenarios returns is the listing's concern.
func TestCommands_MountsOneSubcommandPerScenario(t *testing.T) {
	demo := Commands(noEnv, plainReporter)
	if demo.Name() != "demo" {
		t.Errorf("Commands().Name() = %q; want demo", demo.Name())
	}
	var names []string
	for _, cmd := range demo.Commands() {
		names = append(names, cmd.Name())
	}
	if got := strings.Join(names, ","); got != "domain,problems,sqlate" {
		t.Errorf("Commands() subcommands = %s; want domain,problems,sqlate", got)
	}
}

func TestCommands_PanicsOnADuplicateName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("commands accepted two scenarios sharing a name")
		}
	}()
	commands([]scenario.Scenario{{Name: "stub"}, {Name: "stub"}}, noEnv, plainReporter)
}
