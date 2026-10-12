package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

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
		Flags: func(f *flag.FlagSet) {
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

// execute runs args against a throwaway root that carries a --base root
// flag and mounts both stubs through Command, the way the demo command
// mounts the real scenarios, and returns what the run wrote to stdout and
// stderr and its exit code. The stubs' Source reads the flag rather than a
// node, so the tree declares none and builds nothing.
func execute(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var base string
	root := &cli.Command{Name: "root"}
	root.Flags().StringVar(&base, "base", "http://default.test", "")
	src := scenario.Source{
		Env:      func(*cli.Invocation) env.Env { return env.Env{Base: base} },
		Reporter: func(inv *cli.Invocation) *scenario.Reporter { return scenario.NewReporter(inv.Stdout, false) },
	}
	root.Add(scenario.Command(stubOne, src), scenario.Command(stubTwo, src))

	var out, errOut bytes.Buffer
	code = cli.Run(context.Background(), root, args, cli.Streams{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
	return out.String(), errOut.String(), code
}

func TestCommand_IsNamedForTheScenarioWithItsSummary(t *testing.T) {
	out, _, code := execute(t, "--help")
	if code != process.ExitUsage {
		t.Errorf("--help exited %d, want %d", code, process.ExitUsage)
	}
	if !regexp.MustCompile(`(?m)^  stub:two +the second stub$`).MatchString(out) {
		t.Errorf("help does not list stub:two with its summary:\n%s", out)
	}
	out, _, _ = execute(t, "stub:two", "--help")
	if !strings.Contains(out, "--greeting string") {
		t.Errorf("stub:two's help lacks the scenario's own flag:\n%s", out)
	}
}

func TestCommand_DispatchesToTheNamedScenario(t *testing.T) {
	out, errOut, code := execute(t, "stub:two")
	if code != process.ExitOK {
		t.Fatalf("run stub:two exited %d: %s", code, errOut)
	}
	if !strings.Contains(out, "ran two") || strings.Contains(out, "ran one") {
		t.Errorf("run stub:two dispatched wrongly:\n%s", out)
	}
	if !strings.Contains(out, "[1/1] say which one ran") {
		t.Errorf("run did not narrate the step's intent:\n%s", out)
	}
}

func TestCommand_CarriesTheRootFlagsToTheScenario(t *testing.T) {
	for name, args := range map[string][]string{
		"before": {"--base", "http://example.test:1", "stub:one"},
		"after":  {"stub:one", "--base", "http://example.test:1"},
	} {
		t.Run(name, func(t *testing.T) {
			out, errOut, code := execute(t, args...)
			if code != process.ExitOK {
				t.Fatalf("run stub:one exited %d: %s", code, errOut)
			}
			if !strings.Contains(out, "ran one against http://example.test:1") {
				t.Errorf("the scenario did not see --base:\n%s", out)
			}
		})
	}
}

func TestCommand_AcceptsTheScenarioOwnFlags(t *testing.T) {
	if _, errOut, code := execute(t, "stub:two", "--greeting", "hi"); code != process.ExitOK {
		t.Errorf("run stub:two --greeting exited %d: %s", code, errOut)
	}
	if _, errOut, code := execute(t, "stub:one", "--greeting", "hi"); code != process.ExitUsage || !strings.Contains(errOut, "flag provided but not defined: -greeting") {
		t.Errorf("stub:one --greeting exited %d with %q, want stub:two's flag refused as a usage error", code, errOut)
	}
}

func TestCommand_RejectsAnUnknownScenarioAndArguments(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"an unknown scenario": {[]string{"stub:none"}, `root: unknown command "stub:none"`},
		"an argument":         {[]string{"stub:one", "extra"}, "root stub:one: accepts no arguments, got 1"},
	} {
		t.Run(name, func(t *testing.T) {
			out, errOut, code := execute(t, tc.args...)
			if code != process.ExitUsage || !strings.HasPrefix(errOut, tc.want) {
				t.Errorf("exited %d with %q, want %d starting %q", code, errOut, process.ExitUsage, tc.want)
			}
			if strings.Contains(out, "ran ") {
				t.Errorf("a scenario ran:\n%s", out)
			}
		})
	}
}

// A failing step is the command's error, which the dispatcher reports once
// on stderr with ExitFailure, naming the scenario's command path.
func TestCommand_AFailedRunExitsOne(t *testing.T) {
	failing := scenario.Scenario{
		Name:  "stub:fail",
		Steps: []scenario.Step{{Intent: "fail", Action: func(context.Context, *scenario.Reporter) error { return errors.New("boom") }}},
	}
	root := (&cli.Command{Name: "root"}).Add(scenario.Command(failing, scenario.Source{
		Env:      func(*cli.Invocation) env.Env { return env.Env{} },
		Reporter: func(inv *cli.Invocation) *scenario.Reporter { return scenario.NewReporter(inv.Stdout, false) },
	}))
	var out, errOut bytes.Buffer

	code := cli.Run(context.Background(), root, []string{"stub:fail"}, cli.Streams{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})

	if code != process.ExitFailure {
		t.Errorf("exited %d, want %d", code, process.ExitFailure)
	}
	if want := "root stub:fail: stub:fail: step 1 (fail): boom\n"; errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
}
