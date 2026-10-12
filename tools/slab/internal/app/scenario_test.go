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

// sqlateBuilt is what the sqlate scenario builds: the nodes it declares, in
// the order it declares them, and the lifecycle config the dispatcher adds.
var sqlateBuilt = []string{"repo", "no color", "lifecycle config"}

// The sqlate scenario needs nothing running, so it runs here end to end on
// go-cli-sdk's runner, over the repository the test runs inside: it builds
// the nodes it declares and nothing else, narrates its four steps, and
// shows each authored source, each registration call, and the compiled
// statement with its placeholders.
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
	if strings.Contains(out, "[5/") {
		t.Errorf("the scenario narrates more than four steps:\n%s", out)
	}
	// The compiled SQL block runs from its caption to the next caption; the
	// authored statement and the note under it name the {{ }} markers and
	// are not checked.
	start := strings.Index(out, "create, compiled for execution by postgres")
	end := start + strings.Index(out[start:], "Placeholders, in position order")
	if compiled := out[start:end]; strings.Contains(compiled, "{{") {
		t.Errorf("a {{ marker survived into the compiled text:\n%s", compiled)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("narration to a stream that is no terminal carries ANSI escapes:\n%s", out)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if !slices.Equal(built.Log(), sqlateBuilt) {
		t.Errorf("nodes built = %q, want %q", built.Log(), sqlateBuilt)
	}
}

// A --repo that is no repository fails the first step, which reads it, and
// no later step runs.
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

// On a terminal the blocks are colored and the headings, captions, and
// notes are not; --no-color, SLAB_NO_COLOR, and NO_COLOR each turn the
// blocks' color off.
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

// scenario alone prints its help, which ends with the listing: each
// scenario's name and summary, and the nodes it uses. It builds nothing.
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
	if want := []string{"sqlate", "domain", "problems", "storage"}; !slices.Equal(names, want) {
		t.Errorf("the listing names %q, want %q:\n%s", names, want, listing)
	}
	// Each uses line names the need nodes first, then the settings the
	// steps read: storage points at no trace, so it uses no grafana up.
	if want := []string{
		"repo, no color",
		"service, grafana up, repo, no color",
		"service, grafana up, repo, no color",
		"service, repo, no color",
	}; !slices.Equal(uses, want) {
		t.Errorf("the listing's uses lines are %q, want %q:\n%s", uses, want, listing)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

// Every usage error of scenario exits ExitUsage with go-cli-sdk's report on
// stderr, before anything is built.
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
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

// stackScenarios are the scenarios that declare the service node.
var stackScenarios = []string{"domain", "problems", "storage"}

// closedURL returns the URL of a listener that has been closed, where
// nothing answers.
func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

// answering returns the URL of a server that answers the readiness and
// health probes with their status from probes, keyed by path, and every
// other request with 500, recording each path it is sent.
func answering(t *testing.T, probes map[string]int) (string, *apptest.Recorder) {
	t.Helper()
	sent := &apptest.Recorder{}
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

// A scenario that declares the service node fails before its first step
// when nothing answers at the base URL: the dispatcher reports the
// service node's start once, naming mise run serve, and exits 1.
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

// A service that answers its readiness probe with 503 is up over a
// database or storage that is not: the run fails before its first step,
// naming mise run db:up, and sends the service nothing past the probe.
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

// A scenario that points at traces declares grafana up, so it fails before
// its first step when Grafana does not answer, naming the node and mise
// run otel:up, though the service is ready.
func TestScenario_StopsAtStartWhenGrafanaIsDown(t *testing.T) {
	for _, name := range []string{"domain", "problems"} {
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
