package app_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
	"github.com/standards-lab/go-web-service/tools/slab/internal/apptest"
)

// shutdownTimeout and noColor are variables a Build reads.
var (
	shutdownTimeout = lifecycle.NewEnv("SLAB").ShutdownTimeout
	noColor         = "SLAB_NO_COLOR"
)

// unreadable makes every configuration a Build reads fail, so a run that
// does not fail on it proves it read none.
func unreadable(t *testing.T) {
	t.Helper()
	t.Setenv(shutdownTimeout, "not-a-duration")
	t.Setenv(noColor, "not-a-bool")
}

// newApp returns the App over fresh buffers, and the recorder of the nodes
// its runs build.
func newApp(t *testing.T) (a *app.App, stdout, stderr *bytes.Buffer, built *apptest.Recorder) {
	t.Helper()
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	a = app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr})
	return a, stdout, stderr, apptest.Builds(a)
}

// settings are the values of the run setting nodes, as a probe reads them.
type settings struct {
	base, grafana, tempo, repo string
	noColor                    bool
}

// probe adds a group command at a's root holding three leaves that each
// declare every run setting node: probe, which keeps the settings it read
// and returns where, nil until it runs; needs, which requires --a; and
// either, whose --a and --b are exclusive. The leaves sit below a parent so
// a root flag can be given at every depth.
func probe(a *app.App) **settings {
	got := new(*settings)
	n := a.Nodes()
	nodes := []graph.Ref{n.Base, n.Grafana, n.Tempo, n.Repo, n.NoColor}
	read := (&cli.Command{
		Name:    "probe",
		Summary: "Read the settings",
		Args:    cli.NoArgs,
		Run: func(_ context.Context, inv *cli.Invocation) error {
			*got = &settings{
				base:    inv.Get(n.Base),
				grafana: inv.Get(n.Grafana),
				tempo:   inv.Get(n.Tempo),
				repo:    inv.Get(n.Repo),
				noColor: inv.Get(n.NoColor),
			}
			return nil
		},
	}).Use(nodes...)
	needs := (&cli.Command{Name: "needs", Run: func(context.Context, *cli.Invocation) error { return nil }}).Use(nodes...)
	needs.Flags().String("a", "", "")
	needs.Require("a")
	either := (&cli.Command{Name: "either", Run: func(context.Context, *cli.Invocation) error { return nil }}).Use(nodes...)
	either.Flags().String("a", "", "")
	either.Flags().String("b", "", "")
	either.Exclusive("a", "b")
	a.Root().Add((&cli.Command{Name: "group", Summary: "Hold the probes"}).Add(read, needs, either))
	return got
}

func TestRun_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			unreadable(t)
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			for _, want := range []string{
				"Usage:\n  slab <command> [flags]",
				"Commands:", "version", "org", "docs", "admin", "demo", "list",
				"--base string", `(default "http://127.0.0.1:8080")`, "--grafana string", "--tempo string", "--repo string", "--no-color",
				"Scenarios:", "  sqlate    ", "  domain    ", "  problems  ", "  storage   ",
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("root help lacks %q:\n%s", want, stdout)
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

// A parent run with no subcommand prints its help, as the root does.
func TestRun_ParentWithNoSubcommand(t *testing.T) {
	unreadable(t)
	a, stdout, stderr, built := newApp(t)
	probe(a)

	code := a.Run(context.Background(), []string{"group"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
	}
	if want := "Hold the probes\n\nUsage:\n  slab group <command> [flags]"; !strings.HasPrefix(stdout.String(), want) {
		t.Errorf("stdout = %q, want it to start with %q", stdout, want)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

// Every usage error exits ExitUsage with go-cli-sdk's report on stderr,
// before anything is built.
func TestRun_UsageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"an unknown command":     {[]string{"bogus"}, `slab: unknown command "bogus"`},
		"an unknown subcommand":  {[]string{"group", "bogus"}, `slab group: unknown command "bogus"`},
		"an extra argument":      {[]string{"version", "extra"}, "slab version: accepts no arguments, got 1\nUsage: slab version [flags]\n"},
		"an unknown flag":        {[]string{"version", "--bogus"}, "slab version: flag provided but not defined: -bogus\nUsage: slab version [flags]\n"},
		"a root flag's value":    {[]string{"group", "probe", "--base"}, "slab group probe: flag needs an argument: -base\n"},
		"a missing flag":         {[]string{"group", "needs"}, "slab group needs: required flag --a not set\n"},
		"exclusive flags":        {[]string{"group", "either", "--a", "x", "--b", "y"}, "slab group either: flags --a, --b cannot be used together\n"},
		"a bad root flag syntax": {[]string{"--no-color=maybe", "version"}, "slab: invalid boolean value"},
	} {
		t.Run(name, func(t *testing.T) {
			unreadable(t)
			a, stdout, stderr, built := newApp(t)
			probe(a)

			code := a.Run(context.Background(), tc.args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.HasPrefix(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, tc.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	unreadable(t)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	// A test binary carries no main module version.
	if stdout.String() != "(devel)\n" {
		t.Errorf("stdout = %q, want %q", stdout, "(devel)\n")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

// New builds nothing: the graph freezes on its first Build and panics on a
// Replace after it, so Halt's Replace succeeding proves New never built.
func TestNew_IsCold(t *testing.T) {
	unreadable(t)

	a, stdout, stderr, _ := newApp(t)

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("New() wrote %q, %q, want nothing", stdout, stderr)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Replace after New() panicked: %v; want the graph unbuilt", r)
		}
	}()
	apptest.Halt(a)
}

// probeBuilt is what the probe builds: the run setting nodes it declares,
// in the order it declares them, and the lifecycle config the dispatcher
// adds.
var probeBuilt = []string{"base", "grafana", "tempo", "repo", "no color", "lifecycle config"}

// A command builds the nodes it declares, and the lifecycle config the
// dispatcher adds, and nothing else.
func TestRun_CommandBuildsWhatItDeclares(t *testing.T) {
	a, _, stderr, built := newApp(t)
	got := probe(a)

	code := a.Run(context.Background(), []string{"group", "probe"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if *got == nil {
		t.Fatal("probe did not run")
	}
	if !slices.Equal(built.Log(), probeBuilt) {
		t.Errorf("nodes built = %q, want %q", built.Log(), probeBuilt)
	}
}

// A command that declares one setting builds that setting alone, with the
// lifecycle config, and reads no other: the other settings' variables are
// unreadable or unused, and the run succeeds.
func TestRun_ASettingIsReadOnlyByACommandThatDeclaresIt(t *testing.T) {
	t.Setenv(noColor, "not-a-bool")
	t.Setenv("SLAB_TEMPO", "http://env-tempo")
	a, _, stderr, built := newApp(t)
	tempo := a.Nodes().Tempo
	var got string
	a.Root().Add((&cli.Command{
		Name: "tempo",
		Run: func(_ context.Context, inv *cli.Invocation) error {
			got = inv.Get(tempo)
			return nil
		},
	}).Use(tempo))

	code := a.Run(context.Background(), []string{"tempo"})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if got != "http://env-tempo" {
		t.Errorf("tempo = %q, want the environment's", got)
	}
	if want := []string{"tempo", "lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

// Each setting node reads its defaults, then the SLAB_* variables, then the
// root flags given anywhere on the command line, each later one winning.
func TestSettings_LayerDefaultsEnvAndFlags(t *testing.T) {
	defaults := settings{
		base:    "http://127.0.0.1:8080",
		grafana: "http://127.0.0.1:3000",
		tempo:   "http://127.0.0.1:3200",
	}
	fromEnv := settings{base: "http://env-base", grafana: "http://env-grafana", tempo: "http://env-tempo", repo: "/env-repo", noColor: true}
	fromFlags := settings{base: "http://flag-base", grafana: "http://flag-grafana", tempo: "http://flag-tempo", repo: "/flag-repo", noColor: false}
	flags := []string{"--base", "http://flag-base", "--grafana=http://flag-grafana", "--tempo", "http://flag-tempo", "--repo", "/flag-repo", "--no-color=false"}
	for name, tc := range map[string]struct {
		env  bool
		args []string
		want settings
	}{
		"the defaults":                   {false, []string{"group", "probe"}, defaults},
		"the environment":                {true, []string{"group", "probe"}, fromEnv},
		"flags before the parent":        {true, slices.Concat(flags, []string{"group", "probe"}), fromFlags},
		"flags between parent and leaf":  {true, slices.Concat([]string{"group"}, flags, []string{"probe"}), fromFlags},
		"flags after the leaf":           {true, slices.Concat([]string{"group", "probe"}, flags), fromFlags},
		"flags over the defaults":        {false, slices.Concat([]string{"group", "probe"}, flags), fromFlags},
		"a flag given its default value": {true, []string{"group", "probe", "--base", "http://127.0.0.1:8080"}, func() settings { c := fromEnv; c.base = defaults.base; return c }()},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.env {
				t.Setenv("SLAB_BASE", fromEnv.base)
				t.Setenv("SLAB_GRAFANA", fromEnv.grafana)
				t.Setenv("SLAB_TEMPO", fromEnv.tempo)
				t.Setenv("SLAB_REPO", fromEnv.repo)
				t.Setenv(noColor, "true")
			} else {
				for _, v := range []string{"SLAB_BASE", "SLAB_GRAFANA", "SLAB_TEMPO", "SLAB_REPO", noColor} {
					t.Setenv(v, "")
				}
			}
			a, _, stderr, _ := newApp(t)
			got := probe(a)

			code := a.Run(context.Background(), tc.args)

			if code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			if **got != tc.want {
				t.Errorf("settings = %+v, want %+v", **got, tc.want)
			}
		})
	}
}

// A run that dispatches twice starts the second from the flags' defaults,
// so a flag the first run gave does not override the environment in the
// second.
func TestSettings_FlagsDoNotOutliveTheirRun(t *testing.T) {
	t.Setenv("SLAB_BASE", "http://env-base")
	a, _, stderr, _ := newApp(t)
	got := probe(a)

	if code := a.Run(context.Background(), []string{"group", "probe", "--base", "http://flag-base"}); code != process.ExitOK {
		t.Fatalf("first run code = %d; stderr = %q", code, stderr)
	}
	if code := a.Run(context.Background(), []string{"group", "probe"}); code != process.ExitOK {
		t.Fatalf("second run code = %d; stderr = %q", code, stderr)
	}
	if (*got).base != "http://env-base" {
		t.Errorf("second run base = %q, want the environment's", (*got).base)
	}
}

func TestSettings_UnreadableEnvFailsTheCommand(t *testing.T) {
	t.Setenv(noColor, "not-a-bool")
	a, _, stderr, _ := newApp(t)
	got := probe(a)

	code := a.Run(context.Background(), []string{"group", "probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *got != nil {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "slab group probe: no color: SLAB_NO_COLOR: "; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestRun_InvalidShutdownTimeoutFailsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, _, stderr, _ := newApp(t)
	got := probe(a)

	code := a.Run(context.Background(), []string{"group", "probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *got != nil {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "slab group probe: lifecycle config: SLAB_SHUTDOWN_TIMEOUT"; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

// A halted run of a command that declares nodes fails with ErrHalted
// before the command runs. An unreadable shutdown timeout proves the
// substitute replaced the production constructor.
func TestRun_HaltStopsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, "not-a-duration")
	a, stdout, stderr, built := newApp(t)
	got := probe(a)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"group", "probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *got != nil {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "slab group probe: lifecycle config: " + apptest.ErrHalted.Error(); !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !slices.Equal(built.Log(), probeBuilt) {
		t.Errorf("nodes built = %q, want %q", built.Log(), probeBuilt)
	}
}

// Halt touches only a Build, so a run that builds nothing is unaffected.
func TestRun_HaltLeavesVersion(t *testing.T) {
	a, stdout, stderr, built := newApp(t)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if stdout.String() != "(devel)\n" {
		t.Errorf("stdout = %q, want %q", stdout, "(devel)\n")
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}
