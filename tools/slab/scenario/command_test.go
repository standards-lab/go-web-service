package scenario_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/standards-lab/go-web-service/tools/slab/env"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

// Two stubs stand in for real scenarios: one reads the environment, the other
// declares a flag of its own.
var (
	stubOne = scenario.Scenario{
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
	}
	stubTwo = scenario.Scenario{
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
	}
)

// execute runs args against a throwaway root that carries a --base
// persistent flag and mounts both stubs through Command, the way the demo
// command mounts the real scenarios.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var base string
	root := &cobra.Command{
		Use:           "root",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.PersistentFlags().StringVar(&base, "base", "http://default.test", "")
	runEnv := func() env.Env { return env.Env{Base: base} }
	newReporter := func(w io.Writer) *scenario.Reporter { return scenario.NewReporter(w, false) }
	root.AddCommand(scenario.Command(stubOne, runEnv, newReporter), scenario.Command(stubTwo, runEnv, newReporter))

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestCommand_IsNamedForTheScenarioWithItsSummary(t *testing.T) {
	cmd := scenario.Command(stubTwo, func() env.Env { return env.Env{} }, nil)
	if cmd.Use != "stub:two" || cmd.Short != "the second stub" {
		t.Errorf("Command = %q %q; want the scenario's name and summary", cmd.Use, cmd.Short)
	}
	if cmd.Flags().Lookup("greeting") == nil {
		t.Error("Command did not add the scenario's own flag")
	}
}

func TestCommand_DispatchesToTheNamedScenario(t *testing.T) {
	out, err := execute(t, "stub:two")
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

func TestCommand_CarriesTheRootFlagsToTheScenario(t *testing.T) {
	out, err := execute(t, "--base", "http://example.test:1", "stub:one")
	if err != nil {
		t.Fatalf("run stub:one: %v", err)
	}
	if !strings.Contains(out, "ran one against http://example.test:1") {
		t.Errorf("the scenario did not see --base:\n%s", out)
	}
}

func TestCommand_AcceptsTheScenarioOwnFlags(t *testing.T) {
	if _, err := execute(t, "stub:two", "--greeting", "hi"); err != nil {
		t.Errorf("run stub:two --greeting: %v", err)
	}
	if _, err := execute(t, "stub:one", "--greeting", "hi"); err == nil {
		t.Error("stub:one accepted stub:two's flag")
	}
}

func TestCommand_RejectsAnUnknownScenario(t *testing.T) {
	if _, err := execute(t, "stub:none"); err == nil {
		t.Error("run stub:none succeeded")
	}
}
