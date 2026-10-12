package app_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
)

// execute runs a fresh App over args and returns what it wrote to stdout
// and stderr and the exit code.
func execute(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out bytes.Buffer
	errOut, code := executeTo(t, &out, args...)
	return out.String(), errOut, code
}

// executeTo is execute with stdout supplied by the caller, for a test that
// needs it to be something other than a buffer. It returns what the app
// wrote to stderr and the exit code.
func executeTo(t *testing.T, stdout io.Writer, args ...string) (stderr string, code int) {
	t.Helper()
	var errOut bytes.Buffer
	code = app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &errOut}).Run(context.Background(), args)
	return errOut.String(), code
}

func TestList_PrintsTheScenariosInPresentationOrder(t *testing.T) {
	out, errOut, code := execute(t, "list")
	if code != 0 {
		t.Fatalf("list exited %d: %s", code, errOut)
	}
	sqlate, domain, problems := strings.Index(out, "  sqlate    "), strings.Index(out, "  domain    "), strings.Index(out, "  problems  ")
	storage := strings.Index(out, "  storage   ")
	if sqlate < 0 || domain < sqlate || problems < domain || storage < problems {
		t.Errorf("list does not print sqlate, domain, problems, storage in that order:\n%s", out)
	}
}

func TestDemo_MountsEachScenario(t *testing.T) {
	out, errOut, code := execute(t, "demo")
	if code != 0 {
		t.Fatalf("demo exited %d: %s", code, errOut)
	}
	for _, want := range []string{"Available Commands:", "sqlate", "domain", "problems", "storage"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo help lacks %q:\n%s", want, out)
		}
	}
	if _, errOut, code := execute(t, "demo", "none"); code == 0 || errOut == "" {
		t.Errorf("demo none exited %d with stderr %q; want a failure that says why", code, errOut)
	}
}

// A usage error under cobra is rendered as its message and exits 1, as it
// did before the move.
func TestBridge_RendersTheErrorAndExitsNonZero(t *testing.T) {
	out, errOut, code := execute(t, "list", "--bogus")
	if code != 1 {
		t.Errorf("exited %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing", out)
	}
	if !strings.Contains(errOut, "unknown flag: --bogus") {
		t.Errorf("stderr = %q, want cobra's unknown flag message", errOut)
	}
}

// A root flag may come before the family's word, as it could under cobra:
// the bridge skips it, and the value a non-boolean one takes, to find the
// word, and hands the whole command line to the cobra tree. The listing on
// stdout proves the cobra tree ran list: the tree on go-cli-sdk holds only
// a placeholder for it.
func TestBridge_TakesRootFlagsBeforeTheFamilyWord(t *testing.T) {
	for name, args := range map[string][]string{
		"a valued flag":     {"--base", "http://127.0.0.1:1", "list"},
		"a flag with =":     {"--base=http://127.0.0.1:1", "list"},
		"a boolean flag":    {"--no-color", "--base", "http://127.0.0.1:1", "list"},
		"a value like word": {"--repo", "version", "list"},
	} {
		t.Run(name, func(t *testing.T) {
			out, errOut, code := execute(t, args...)
			if code != 0 {
				t.Fatalf("exited %d: %s", code, errOut)
			}
			if !strings.Contains(out, "  sqlate    ") {
				t.Errorf("stdout lacks the listing:\n%s", out)
			}
		})
	}
}
