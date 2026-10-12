package app_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/internal/apptest"
)

// list prints the listing in presentation order wherever the root flags
// fall, builds nothing, and so reads no configuration.
func TestList_PrintsTheScenariosInPresentationOrder(t *testing.T) {
	for name, args := range map[string][]string{
		"alone":                 {"list"},
		"a root flag before":    {"--base", "http://127.0.0.1:1", "list"},
		"a root flag after":     {"list", "--no-color"},
		"after a -- separator":  {"--", "list"},
		"a value like the word": {"--repo", "version", "list"},
	} {
		t.Run(name, func(t *testing.T) {
			unreadable(t)
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), args)

			if code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			out := stdout.String()
			sqlate, domain, problems, storage := strings.Index(out, "  sqlate    "), strings.Index(out, "  domain    "), strings.Index(out, "  problems  "), strings.Index(out, "  storage   ")
			if sqlate != 0 || domain < sqlate || problems < domain || storage < problems {
				t.Errorf("list does not print sqlate, domain, problems, storage in that order:\n%s", out)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

// demo alone prints its help, listing each scenario, as any parent does.
func TestDemo_WithNoScenarioPrintsItsHelp(t *testing.T) {
	unreadable(t)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"demo"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
	}
	for _, want := range []string{"Run one scenario\n\nUsage:\n  slab demo <command> [flags]", "  sqlate ", "  domain ", "  problems ", "  storage "} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("demo help lacks %q:\n%s", want, stdout)
		}
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

// Every usage error of demo and list exits ExitUsage with go-cli-sdk's
// report on stderr, before anything is built.
func TestDemoAndList_UsageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"an unknown scenario":       {[]string{"demo", "none"}, `slab demo: unknown command "none"`},
		"a scenario's argument":     {[]string{"demo", "sqlate", "extra"}, "slab demo sqlate: accepts no arguments, got 1\nUsage: slab demo sqlate [flags]\n"},
		"a scenario's unknown flag": {[]string{"demo", "sqlate", "--bogus"}, "slab demo sqlate: flag provided but not defined: -bogus\nUsage: slab demo sqlate [flags]\n"},
		"list's argument":           {[]string{"list", "extra"}, "slab list: accepts no arguments, got 1\nUsage: slab list [flags]\n"},
		"list's unknown flag":       {[]string{"list", "--bogus"}, "slab list: flag provided but not defined: -bogus\nUsage: slab list [flags]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			unreadable(t)
			a, _, stderr, built := newApp(t)

			code := a.Run(context.Background(), tc.args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.HasPrefix(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, tc.want)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

// The sqlate scenario needs nothing running, so it runs here end to end on
// slab's own runner, over the repository the test runs inside: it builds
// the config node it declares, and the lifecycle config, and narrates.
func TestDemo_RunsAScenario(t *testing.T) {
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"demo", "sqlate", "--no-color"})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	for _, want := range []string{"[1/4] Write a pattern", "[4/4] Show the compiled output"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the narration lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Errorf("--no-color narration carries ANSI escapes:\n%s", stdout)
	}
	if want := []string{"config", "lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

// A scenario that fails exits ExitFailure with its error reported once on
// stderr. --repo pointed at a directory that is not the repository fails
// the first step, which proves the root flags reach the scenario's
// environment through the config node.
func TestDemo_AFailedScenarioExitsOne(t *testing.T) {
	a, _, stderr, _ := newApp(t)

	code := a.Run(context.Background(), []string{"demo", "sqlate", "--repo", t.TempDir()})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if want := "slab demo sqlate: sqlate: step 1 (Write a pattern): repo: --repo "; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if n := strings.Count(stderr.String(), "\n"); n != 1 {
		t.Errorf("stderr has %d lines, want the one report:\n%s", n, stderr)
	}
}

// A halted demo run fails its Build before any scenario step runs.
func TestDemo_HaltStopsTheScenario(t *testing.T) {
	a, stdout, stderr, _ := newApp(t)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"demo", "sqlate"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if want := "slab demo sqlate: lifecycle config: " + apptest.ErrHalted.Error(); !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no narration", stdout)
	}
}
