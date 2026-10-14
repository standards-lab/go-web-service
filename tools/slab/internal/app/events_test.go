package app_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-web-service/tools/slab/repo"
)

// natsSettings are the values of the nats setting nodes, as a probe reads
// them.
type natsSettings struct {
	URL    string `json:"url"`
	Stream string `json:"stream"`
	Prefix string `json:"prefix"`
}

// natsVariables are the variables the nats settings read.
var natsVariables = []string{"SLAB_NATS", "SLAB_NATS_STREAM", "SLAB_NATS_PREFIX"}

// closedNATS returns the URL of a TCP listener that has been closed, where
// no NATS server answers.
func closedNATS(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "nats://" + addr
}

// serviceNATS returns the nats block of the service's base configuration,
// the repository root's config.json.
func serviceNATS(t *testing.T) natsSettings {
	t.Helper()
	root, err := repo.Find(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		NATS natsSettings `json:"nats"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.NATS
}

// The nats settings default to the service's base configuration, so a
// watch with no flags reads the stream the service publishes to; each
// reads its SLAB_* variable over the default and its root flag over that.
func TestEvents_NATSSettingsDefaultToTheServiceConfiguration(t *testing.T) {
	service := serviceNATS(t)
	fromEnv := natsSettings{URL: "nats://env:4222", Stream: "ENV", Prefix: "env"}
	fromFlags := natsSettings{URL: "nats://flag:4222", Stream: "FLAG", Prefix: "flag"}
	flags := []string{"--nats", fromFlags.URL, "--nats-stream=" + fromFlags.Stream, "--nats-prefix", fromFlags.Prefix}
	for name, tc := range map[string]struct {
		env  bool
		args []string
		want natsSettings
	}{
		"the defaults":    {false, []string{"nats"}, service},
		"the environment": {true, []string{"nats"}, fromEnv},
		"the flags":       {true, append([]string{"nats"}, flags...), fromFlags},
	} {
		t.Run(name, func(t *testing.T) {
			values := []string{"", "", ""}
			if tc.env {
				values = []string{fromEnv.URL, fromEnv.Stream, fromEnv.Prefix}
			}
			for i, v := range natsVariables {
				t.Setenv(v, values[i])
			}
			a, _, stderr, _ := newApp(t)
			n := a.Nodes()
			var got natsSettings
			a.Root().Add((&cli.Command{
				Name: "nats",
				Run: func(_ context.Context, inv *cli.Invocation) error {
					got = natsSettings{URL: inv.Get(n.NATS), Stream: inv.Get(n.NATSStream), Prefix: inv.Get(n.NATSPrefix)}
					return nil
				},
			}).Use(n.NATS, n.NATSStream, n.NATSPrefix))

			if code := a.Run(context.Background(), tc.args); code != process.ExitOK {
				t.Fatalf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
			}
			if got != tc.want {
				t.Errorf("nats settings = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// With no NATS server answering, the watch fails at its start, before it
// watches anything: one report naming the broker node and the mise task
// that brings the stack up, exit code 1, and nothing on stdout.
func TestEvents_WatchReportsNoServerNamingTheTaskThatBringsItUp(t *testing.T) {
	for _, args := range [][]string{{"events", "watch"}, {"events", "watch", "--from-start"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			url := closedNATS(t)
			a, stdout, stderr, _ := newApp(t)

			code := a.Run(context.Background(), append(args, "--nats", url))

			if code != process.ExitFailure {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitFailure, stderr)
			}
			if want := "slab events watch: startup: broker: nothing answers at " + url + " (start the stack with: mise run db:up): "; !strings.HasPrefix(stderr.String(), want) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, want)
			}
			if n := strings.Count(stderr.String(), "slab events watch"); n != 1 {
				t.Errorf("stderr reports %d times, want once:\n%s", n, stderr)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

// A setting go-messaging's NATS configuration refuses fails the broker's
// Build, before anything connects.
func TestEvents_WatchRefusesAnInvalidStream(t *testing.T) {
	a, stdout, stderr, _ := newApp(t)

	code := a.Run(context.Background(), []string{"events", "watch", "--nats", closedNATS(t), "--nats-stream", "a.b"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitFailure, stderr)
	}
	if want := `slab events watch: broker: nats: config: stream: "a.b" must not contain`; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// The watch's usage errors and its help build nothing, so nothing
// connects: an argument, an unknown flag, a value given to --from-start,
// and the events parent run alone, whose help lists watch.
func TestEvents_UsageAndHelpBuildNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stdout string
		stderr string
	}{
		"an argument":            {[]string{"events", "watch", "x"}, process.ExitUsage, "", "slab events watch: accepts no arguments, got 1\nUsage: slab events watch"},
		"an unknown flag":        {[]string{"events", "watch", "--from"}, process.ExitUsage, "", "slab events watch: flag provided but not defined: -from"},
		"a non-boolean value":    {[]string{"events", "watch", "--from-start=maybe"}, process.ExitUsage, "", "slab events watch: invalid boolean value"},
		"the parent alone":       {[]string{"events"}, process.ExitUsage, "Watch the events the service publishes\n\nUsage:\n  slab events <command> [flags]", ""},
		"the watch's own --help": {[]string{"events", "watch", "--help"}, process.ExitUsage, "Print each event the service publishes as it arrives, until interrupted", ""},
	} {
		t.Run(name, func(t *testing.T) {
			unreadable(t)
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), tc.args)

			if code != tc.code {
				t.Errorf("code = %d, want %d; stderr = %q", code, tc.code, stderr)
			}
			if !strings.HasPrefix(stdout.String(), tc.stdout) || (tc.stdout == "") != (stdout.Len() == 0) {
				t.Errorf("stdout = %q, want it to start with %q", stdout, tc.stdout)
			}
			if !strings.Contains(stderr.String(), tc.stderr) || (tc.stderr == "") != (stderr.Len() == 0) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.stderr)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
	a, stdout, _, _ := newApp(t)
	a.Run(context.Background(), []string{"events"})
	if !strings.Contains(stdout.String(), "\n  watch ") {
		t.Errorf("events help lacks watch:\n%s", stdout)
	}
	a, stdout, _, _ = newApp(t)
	a.Run(context.Background(), []string{"events", "watch", "--help"})
	if !strings.Contains(stdout.String(), "--from-start") {
		t.Errorf("watch help lacks --from-start:\n%s", stdout)
	}
}
