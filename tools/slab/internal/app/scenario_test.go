package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
	"github.com/standards-lab/go-web-service/tools/slab/internal/apptest"
	"github.com/standards-lab/go-web-service/tools/slab/internal/termtest"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

var sqlateBuilt = []string{"repo", "no color", "lifecycle config"}

func TestScenario_RunsSqlate(t *testing.T) {
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"scenario", "sqlate"})

	if code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	out := stdout.String()
	for _, want := range []string{
		"[1/4] Write a pattern",
		"[2/4] Write a statement",
		"[3/4] Catalog registration",
		"[4/4] Show the compiled output",
		"  data/patterns/identity.sql\n",
		"    RETURNING id, version",
		"    //go:embed patterns/*.sql\n    var patterns embed.FS\n",
		"  domain/organization/statements/create.sql\n",
		"    VALUES ({{parent_id:uuid}}, {{code}}, {{name}})",
		"    {{> app.identity}}",
		"    //go:embed statements/*.sql\n    var statements embed.FS\n",
		"  fsys, the filesystem both calls below read from\n    fsys, err := repo.FS(override)\n",
		`    query.NewCatalog(query.Patterns(), bfdata.Patterns(), query.Publish("app", fsys, "data/patterns"))`,
		`    catalog.Compile(fsys, "domain/organization/statements", postgres.Dialect{})`,
		"  create, compiled for execution by postgres\n",
		"    VALUES (CAST($1 AS uuid), $2, $3)",
		"  Placeholders, in position order\n    $1  parent_id\n    $2  code\n    $3  name\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the narration lacks %q:\n%s", want, out)
		}
	}
	setOff(t, out, []block{
		{"data/patterns/identity.sql", "RETURNING id, version"},
		{"An embed directive over an embed.FS variable", "var patterns embed.FS"},
		{"domain/organization/statements/create.sql", "{{> app.identity}}"},
		{"An embed directive over an embed.FS variable", "var statements embed.FS"},
		{"fsys, the filesystem both calls below read from", "fsys, err := repo.FS(override)"},
		{"The pattern registered under the", `query.Publish("app", fsys, "data/patterns"))`},
		{"A directory of statements, compiled against the postgres dialect", `postgres.Dialect{})`},
		{"create, compiled for execution by postgres", "RETURNING id, version"},
		{"Placeholders, in position order", "$3  name"},
	})
	if strings.Contains(out, "[5/") {
		t.Errorf("the scenario narrates more than four steps:\n%s", out)
	}
	compiledCaption := strings.Index(out, "create, compiled for execution by postgres")
	nextCaption := compiledCaption + strings.Index(out[compiledCaption:], "Placeholders, in position order")
	if compiledBlock := out[compiledCaption:nextCaption]; strings.Contains(compiledBlock, "{{") {
		t.Errorf("a {{ marker survived into the compiled block:\n%s", compiledBlock)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("narration to a stream that is no terminal carries ANSI escapes:\n%s", out)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if !slices.Equal(apptest.Names(built.Log()), sqlateBuilt) {
		t.Errorf("nodes built = %q, want %q", apptest.Names(built.Log()), sqlateBuilt)
	}
}

type block struct{ caption, last string }

func setOff(t *testing.T, out string, blocks []block) {
	t.Helper()
	at := 0
	for _, b := range blocks {
		caption := strings.Index(out[at:], "  "+b.caption)
		if caption < 0 {
			t.Fatalf("the caption %q is missing after offset %d:\n%s", b.caption, at, out)
		}
		caption += at
		last := strings.Index(out[caption:], b.last)
		if last < 0 {
			t.Fatalf("the block under %q lacks %q:\n%s", b.caption, b.last, out)
		}
		at = caption + last + len(b.last)
		if end := strings.IndexByte(out[at:], '\n'); end >= 0 {
			at += end
		} else {
			at = len(out)
		}
		if !strings.HasSuffix(out[:caption], "\n\n") {
			t.Errorf("no blank line precedes the caption %q:\n%s", b.caption, out)
		}
		if !strings.HasPrefix(out[at:], "\n\n") {
			t.Errorf("no blank line follows the block under %q:\n%s", b.caption, out)
		}
	}
}

func TestScenario_SqlateStopsAtTheFirstStepWhenTheRepoIsNotARoot(t *testing.T) {
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), []string{"scenario", "sqlate", "--repo", t.TempDir()})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitFailure, stderr)
	}
	if !strings.Contains(stdout.String(), "[1/4]") || strings.Contains(stdout.String(), "[2/4]") {
		t.Errorf("run did not stop at step 1:\n%s", stdout)
	}
	if want := "slab scenario sqlate: step 1 (Write a pattern): repo: --repo "; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestScenario_ColorsTheBlocksOnATerminal(t *testing.T) {
	on := style.New(true)
	coloredSQL := on.SQL("RETURNING id, version")
	for name, tc := range map[string]struct {
		args    []string
		env     map[string]string
		colored bool
	}{
		"by default":    {nil, nil, true},
		"--no-color":    {[]string{"--no-color"}, nil, false},
		"SLAB_NO_COLOR": {nil, map[string]string{"SLAB_NO_COLOR": "true"}, false},
		"NO_COLOR":      {nil, map[string]string{"NO_COLOR": "1"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			term := termtest.Open(t)
			stderr := new(strings.Builder)
			a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: term.Writer(), Stderr: stderr})

			code := a.Run(context.Background(), append([]string{"scenario", "sqlate"}, tc.args...))

			if code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			out := term.Output()
			if got := strings.Contains(out, coloredSQL); got != tc.colored {
				t.Errorf("blocks colored = %v, want %v:\n%q", got, tc.colored, out)
			}
			if !tc.colored && strings.Contains(out, "\x1b[") {
				t.Errorf("uncolored narration carries ANSI escapes:\n%q", out)
			}
			for _, plain := range []string{"[1/4] Write a pattern", "  data/patterns/identity.sql\r\n", "  The SQL pattern files are embedded"} {
				if !strings.Contains(out, plain) {
					t.Errorf("narration lacks the plain line %q:\n%q", plain, out)
				}
			}
		})
	}
}

func TestScenario_WithNoScenarioPrintsItsHelpEndingInTheListing(t *testing.T) {
	unreadable(t)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"scenario"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
	}
	out := stdout.String()
	if want := "Run a narrated scenario over the commands\n\nUsage:\n  slab scenario <command> [flags]"; !strings.HasPrefix(out, want) {
		t.Errorf("stdout = %q, want it to start with %q", out, want)
	}
	listing := out[strings.LastIndex(out, "Scenarios:\n"):]
	var names, uses []string
	for line := range strings.Lines(strings.TrimPrefix(listing, "Scenarios:\n")) {
		if rest, ok := strings.CutPrefix(line, "             uses "); ok {
			uses = append(uses, strings.TrimSuffix(rest, "\n"))
			continue
		}
		names = append(names, strings.Fields(line)[0])
	}
	if want := []string{"sqlate", "domain", "problems", "storage", "events", "sweeper"}; !slices.Equal(names, want) {
		t.Errorf("the listing names %q, want %q:\n%s", names, want, listing)
	}
	if needsThenSettings := []string{
		"repo, no color",
		"service, grafana up, repo, no color",
		"service, grafana up, repo, no color",
		"service, repo, no color",
		"service, repo, no color",
		"service, grafana up, repo, no color",
	}; !slices.Equal(uses, needsThenSettings) {
		t.Errorf("the listing's uses lines are %q, want the need nodes, then the settings: %q:\n%s", uses, needsThenSettings, listing)
	}
	if log := apptest.Names(built.Log()); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestScenario_RootHelpEndsWithTheScenarioListing(t *testing.T) {
	unreadable(t)
	help := func(args ...string) string {
		a, stdout, stderr, _ := newApp(t)
		if code := a.Run(context.Background(), args); code != process.ExitUsage {
			t.Fatalf("%q: code = %d, want %d; stderr = %q", args, code, process.ExitUsage, stderr)
		}
		return stdout.String()
	}
	root, parent := help(), help("scenario")

	i := strings.LastIndex(parent, "Scenarios:\n")
	if i < 0 {
		t.Fatalf("scenario help has no listing:\n%s", parent)
	}
	listing := parent[i:]
	if !strings.HasSuffix(root, "\n"+listing) {
		t.Errorf("root help does not end with the scenario listing %q:\n%s", listing, root)
	}
}

func TestScenario_UsageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"an unknown scenario":       {[]string{"scenario", "nosuch"}, `slab scenario: unknown command "nosuch"`},
		"a scenario's argument":     {[]string{"scenario", "sqlate", "extra"}, "slab scenario sqlate: accepts no arguments, got 1\nUsage: slab scenario sqlate [flags]\n"},
		"a scenario's unknown flag": {[]string{"scenario", "sqlate", "--bogus"}, "slab scenario sqlate: flag provided but not defined: -bogus\nUsage: slab scenario sqlate [flags]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			unreadable(t)
			a, stdout, stderr, built := newApp(t)

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
			if log := apptest.Names(built.Log()); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

var stackScenarios = []string{"domain", "problems", "storage", "events", "sweeper"}

func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

func answering(t *testing.T, probes map[string]int) (string, *apptest.Recorder[string]) {
	t.Helper()
	sent := &apptest.Recorder[string]{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Record(r.Method + " " + r.URL.Path)
		status, ok := probes[r.URL.Path]
		if !ok {
			status = http.StatusInternalServerError
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, sent
}

func TestScenario_StopsAtStartWhenTheServiceDoesNotAnswer(t *testing.T) {
	for _, name := range stackScenarios {
		t.Run(name, func(t *testing.T) {
			grafana, _ := answering(t, map[string]int{"/api/health": http.StatusOK})
			r := runScenario(t, name, closedURL(t), grafana)

			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if want := "slab scenario " + name + ": startup: service: nothing answers at "; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if !strings.Contains(r.stderr, "mise run serve") {
				t.Errorf("stderr = %q, want it to name mise run serve", r.stderr)
			}
			if n := strings.Count(r.stderr, "slab scenario"); n != 1 {
				t.Errorf("stderr reports %d times, want once:\n%s", n, r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want no step narrated", r.stdout)
			}
		})
	}
}

func TestScenario_StopsAtStartWhenTheServiceIsNotReady(t *testing.T) {
	for _, name := range stackScenarios {
		t.Run(name, func(t *testing.T) {
			base, sent := answering(t, map[string]int{"/readyz": http.StatusServiceUnavailable, "/api/health": http.StatusOK})
			r := runScenario(t, name, base, base)

			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if want := "slab scenario " + name + ": startup: service: " + base + " answers but is not ready"; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			for _, want := range []string{"mise run db:up", "GET /readyz: status 503 Service Unavailable"} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr = %q, want it to name %q", r.stderr, want)
				}
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want no step narrated", r.stdout)
			}
			if log := sent.Log(); slices.ContainsFunc(log, func(s string) bool { return s != "GET /readyz" && s != "GET /api/health" }) {
				t.Errorf("the service was sent %q, want only the probes", log)
			}
		})
	}
}

func TestScenario_StopsAtStartWhenGrafanaIsDown(t *testing.T) {
	for _, name := range []string{"domain", "problems", "sweeper"} {
		t.Run(name, func(t *testing.T) {
			base, _ := answering(t, map[string]int{"/readyz": http.StatusOK})
			r := runScenario(t, name, base, closedURL(t))

			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if want := "slab scenario " + name + ": startup: grafana up: Grafana is not up at "; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if !strings.Contains(r.stderr, "mise run otel:up") {
				t.Errorf("stderr = %q, want it to name mise run otel:up", r.stderr)
			}
			if strings.Contains(r.stderr, "service:") {
				t.Errorf("stderr = %q, want the ready service unreported", r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want no step narrated", r.stdout)
			}
		})
	}
}

func TestScenario_StopsAtStartOnTheServiceWhenNothingIsRunning(t *testing.T) {
	for _, name := range []string{"domain", "problems", "sweeper"} {
		t.Run(name, func(t *testing.T) {
			r := runScenario(t, name, closedURL(t), closedURL(t))

			if r.code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", r.code, process.ExitFailure, r.stderr)
			}
			if want := "slab scenario " + name + ": startup: service: nothing answers at "; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if !strings.Contains(r.stderr, "mise run serve") {
				t.Errorf("stderr = %q, want it to name mise run serve", r.stderr)
			}
			for _, unwanted := range []string{"grafana up", "mise run otel:up"} {
				if strings.Contains(r.stderr, unwanted) {
					t.Errorf("stderr = %q, want it not to name %q", r.stderr, unwanted)
				}
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want no step narrated", r.stdout)
			}
		})
	}
}
