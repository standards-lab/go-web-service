package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/standards-lab/go-web-service/tools/slab/internal/cli"
	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
	"github.com/standards-lab/go-web-service/tools/slab/internal/scenario"
)

// Two stubs stand in for real scenarios: the registry is package state, so
// they register once for the whole test binary.
func init() {
	scenario.Add(scenario.Scenario{
		Name:    "stub:one",
		Summary: "the first stub",
		Needs: []scenario.Need{{
			What:  "a stub dependency",
			Task:  "stub-up",
			Check: func(context.Context) error { return nil },
		}},
		Steps: []scenario.Step{{
			Intent: "say which one ran",
			Action: func(ctx context.Context, r *scenario.Reporter) error {
				r.Note("ran one against %s", env.FromContext(ctx).Base)
				return nil
			},
		}},
	})
	scenario.Add(scenario.Scenario{
		Name:    "stub:two",
		Summary: "the second stub",
		Flags: func(f *pflag.FlagSet) {
			f.String("greeting", "hello", "what two says")
		},
		Steps: []scenario.Step{{
			Intent: "say which one ran",
			Action: func(_ context.Context, r *scenario.Reporter) error {
				r.Note("ran two")
				return nil
			},
		}},
	})
}

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := cli.Root()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestAll_KeepsRegistrationOrder(t *testing.T) {
	var names []string
	for _, s := range scenario.All() {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "stub:one,stub:two" {
		t.Errorf("All() = %s; want stub:one,stub:two", got)
	}
}

func TestLookup_FindsByName(t *testing.T) {
	s, ok := scenario.Lookup("stub:two")
	if !ok || s.Summary != "the second stub" {
		t.Errorf("Lookup(stub:two) = %+v, %v", s, ok)
	}
	if _, ok := scenario.Lookup("stub:none"); ok {
		t.Error("Lookup found a scenario that was never registered")
	}
}

func TestList_NamesEachScenarioWithItsSummaryAndNeeds(t *testing.T) {
	out, err := execute(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{
		"stub:one  the first stub",
		"stub:two  the second stub",
		"needs a stub dependency (mise run stub-up)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}
}

func TestRoot_PrintsHelpAndTheListing(t *testing.T) {
	out, err := execute(t)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	for _, want := range []string{"Available Commands:", "list", "demo", "stub:one  the first stub"} {
		if !strings.Contains(out, want) {
			t.Errorf("root output lacks %q:\n%s", want, out)
		}
	}
}

func TestRun_DispatchesToTheNamedScenario(t *testing.T) {
	out, err := execute(t, "demo", "stub:two")
	if err != nil {
		t.Fatalf("run stub:two: %v", err)
	}
	if !strings.Contains(out, "ran two") || strings.Contains(out, "ran one") {
		t.Errorf("run stub:two dispatched wrongly:\n%s", out)
	}
	if !strings.Contains(out, "[1/1] say which one ran") {
		t.Errorf("run did not narrate the step's intent:\n%s", out)
	}
}

func TestRun_CarriesTheRootFlagsToTheScenario(t *testing.T) {
	out, err := execute(t, "--base", "http://example.test:1", "demo", "stub:one")
	if err != nil {
		t.Fatalf("run stub:one: %v", err)
	}
	if !strings.Contains(out, "ran one against http://example.test:1") {
		t.Errorf("the scenario did not see --base:\n%s", out)
	}
}

func TestRun_AcceptsTheScenarioOwnFlags(t *testing.T) {
	if _, err := execute(t, "demo", "stub:two", "--greeting", "hi"); err != nil {
		t.Errorf("run stub:two --greeting: %v", err)
	}
	if _, err := execute(t, "demo", "stub:one", "--greeting", "hi"); err == nil {
		t.Error("stub:one accepted stub:two's flag")
	}
}

func TestRun_RejectsAnUnknownScenario(t *testing.T) {
	if _, err := execute(t, "demo", "stub:none"); err == nil {
		t.Error("run stub:none succeeded")
	}
}

func TestRun_StopsAtAFailedNeed(t *testing.T) {
	boom := errors.New("connection refused")
	var ran bool
	s := scenario.Scenario{
		Name: "stub:needy",
		Needs: []scenario.Need{{
			What:  "the service",
			Task:  "serve",
			Check: func(context.Context) error { return boom },
		}},
		Steps: []scenario.Step{{
			Intent: "never reached",
			Action: func(context.Context, *scenario.Reporter) error { ran = true; return nil },
		}},
	}
	var out bytes.Buffer
	err := scenario.Run(context.Background(), s, scenario.NewReporter(&out, false))
	if !errors.Is(err, boom) {
		t.Errorf("Run error = %v; want the need's error", err)
	}
	if ran {
		t.Error("Run ran a step after a need failed")
	}
	if !strings.Contains(out.String(), "start it with: mise run serve") {
		t.Errorf("Run did not name the task that satisfies the need:\n%s", out.String())
	}
}

func TestAdd_PanicsOnADuplicateName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Add accepted a duplicate name")
		}
	}()
	scenario.Add(scenario.Scenario{Name: "stub:one"})
}
