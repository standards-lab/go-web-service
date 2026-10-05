package demo

import (
	"io"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Pending the architect's decision: the duplicate-name panic doc.go states
// is reachable only through the unexported commands, since Commands builds
// over the fixed Scenarios list. .golangci.yml excludes this file from
// testpackage until it is decided.
func TestCommands_PanicsOnADuplicateName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("commands accepted two scenarios sharing a name")
		}
	}()
	noEnv := func() env.Env { return env.Env{} }
	plain := func(w io.Writer) *scenario.Reporter { return scenario.NewReporter(w, false) }
	commands([]scenario.Scenario{{Name: "stub"}, {Name: "stub"}}, noEnv, plain)
}
