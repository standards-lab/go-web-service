package input_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/input"
)

// fields is a stand-in for the wire type a command builds from its flags.
type fields struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// parsed dispatches a command with the --body and --version flags every
// body-sending command binds over args, and returns the Invocation it ran
// with, along with the flag values as the command's Validate would hold
// them.
func parsed(t *testing.T, args ...string) (inv *cli.Invocation, raw string, version int64) {
	t.Helper()
	cmd := &cli.Command{Name: "x", Run: func(_ context.Context, got *cli.Invocation) error {
		inv = got
		return nil
	}}
	cmd.Flags().StringVar(&raw, "body", "", "")
	cmd.Flags().Int64Var(&version, "version", 0, "")
	var stderr bytes.Buffer
	if code := cli.Run(context.Background(), cmd, args, cli.Streams{Stdout: &stderr, Stderr: &stderr}); code != 0 {
		t.Fatalf("dispatch of %q exited %d: %s", args, code, stderr.String())
	}
	return inv, raw, version
}

// parsedCobra is parsed for a command still on cobra.
func parsedCobra(t *testing.T, args ...string) (cmd *cobra.Command, raw string, version int64) {
	t.Helper()
	cmd = &cobra.Command{Use: "x"}
	cmd.Flags().StringVar(&raw, "body", "", "")
	cmd.Flags().Int64Var(&version, "version", 0, "")
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd, raw, version
}

// built is a fromFlags that returns v.
func built(v any) func() (any, error) {
	return func() (any, error) { return v, nil }
}

// never is a fromFlags that fails the test if called: the --body path does
// not consult the field flags.
func never(t *testing.T) func() (any, error) {
	return func() (any, error) {
		t.Error("fromFlags was called on the --body path")
		return nil, nil
	}
}

func TestBody_SendsTheBodyFlagVerbatim(t *testing.T) {
	for _, raw := range []string{
		`{ "code":"x" , "name":"y" }`,
		`{"code":"x","name":"y"}`,
		"not json at all",
		"",
	} {
		inv, got, _ := parsed(t, "--body", raw)
		body, err := input.Body(inv, got, never(t))
		if err != nil {
			t.Fatalf("Body(--body %q) = %v", raw, err)
		}
		if string(body) != raw {
			t.Errorf("Body(--body %q) = %q, want the bytes as given", raw, body)
		}
	}
}

func TestBody_MarshalsWhatTheFlagsBuild(t *testing.T) {
	inv, raw, _ := parsed(t)
	body, err := input.Body(inv, raw, built(fields{Code: "x", Name: "y"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":"x","name":"y"}`; string(body) != want {
		t.Errorf("Body() = %s, want %s", body, want)
	}
}

func TestBody_ReturnsTheFlagsPathError(t *testing.T) {
	inv, raw, _ := parsed(t)
	want := errors.New("--code and --name are required unless --body is given")
	body, err := input.Body(inv, raw, func() (any, error) { return nil, want })
	if err != want {
		t.Errorf("err = %v, want fromFlags's error as is", err)
	}
	if body != nil {
		t.Errorf("body = %q, want nil", body)
	}
}

func TestBody_ReturnsTheMarshalError(t *testing.T) {
	inv, raw, _ := parsed(t)
	if _, err := input.Body(inv, raw, built(make(chan int))); err == nil {
		t.Error("Body marshaled a channel without error")
	}
}

func TestGuardedBody_ResolvesTheVersionFromTheFlagOrTheBody(t *testing.T) {
	for name, tc := range map[string]struct {
		args    []string
		version int64
		body    string
	}{
		"flags path": {
			[]string{"--version", "3"},
			3, `{"code":"x","name":"y"}`,
		},
		"body carries the version": {
			[]string{"--body", `{"name":"y","code":"x","version":4}`},
			4, `{"code":"x","name":"y"}`,
		},
		"flag wins over the body and the key is still stripped": {
			[]string{"--body", `{"code":"x","name":"y","version":4}`, "--version", "9"},
			9, `{"code":"x","name":"y"}`,
		},
		"flag wins over a body whose version is not a number": {
			[]string{"--body", `{"code":"x","version":"4"}`, "--version", "9"},
			9, `{"code":"x"}`,
		},
		"flag with a body lacking the key sends the body untouched": {
			[]string{"--body", `{ "code": "x", "name": "y" }`, "--version", "2"},
			2, `{ "code": "x", "name": "y" }`,
		},
		"flag with an empty object": {
			[]string{"--body", `{}`, "--version", "1"},
			1, `{}`,
		},
		"body with only the version": {
			[]string{"--body", `{"version":5}`},
			5, `{}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			inv, raw, version := parsed(t, tc.args...)
			fromFlags := never(t)
			if !inv.Changed("body") {
				fromFlags = built(fields{Code: "x", Name: "y"})
			}
			body, ifMatch, err := input.GuardedBody(inv, raw, version, fromFlags)
			if err != nil {
				t.Fatal(err)
			}
			if ifMatch != tc.version {
				t.Errorf("version = %d, want %d", ifMatch, tc.version)
			}
			if string(body) != tc.body {
				t.Errorf("body = %s, want %s", body, tc.body)
			}
		})
	}
}

func TestGuardedBody_RefusesARequestWithoutAVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"flags path":         {nil, "required flag --version not set"},
		"body without key":   {[]string{"--body", `{"code":"x","name":"y"}`}, `--version is required, as the flag or as a top-level "version" number in --body`},
		"body with a string": {[]string{"--body", `{"code":"x","version":"4"}`}, `--body: "version" is not an integer: "4"`},
		"body with a float":  {[]string{"--body", `{"version":1.5}`}, `--body: "version" is not an integer: 1.5`},
		"body not an object": {[]string{"--body", `[1]`}, "--body is not a JSON object"},
		"body not JSON":      {[]string{"--body", `nope`}, "--body is not a JSON object"},
	} {
		inv, raw, version := parsed(t, tc.args...)
		body, ifMatch, err := input.GuardedBody(inv, raw, version, built(fields{}))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if body != nil || ifMatch != 0 {
			t.Errorf("%s: returned body %q and version %d alongside the error", name, body, ifMatch)
		}
	}
}

func TestGuardedBody_ReturnsTheFlagsPathError(t *testing.T) {
	inv, raw, version := parsed(t, "--version", "1")
	want := errors.New("--parent-id is required unless --body is given")
	body, ifMatch, err := input.GuardedBody(inv, raw, version, func() (any, error) { return nil, want })
	if err != want {
		t.Errorf("err = %v, want fromFlags's error as is", err)
	}
	if body != nil || ifMatch != 0 {
		t.Errorf("returned body %q and version %d alongside the error", body, ifMatch)
	}
}

func TestGuardedBody_ChecksTheVersionBeforeBuildingFromFlags(t *testing.T) {
	inv, raw, version := parsed(t)
	_, _, err := input.GuardedBody(inv, raw, version, never(t))
	if err == nil || !strings.Contains(err.Error(), "required flag --version not set") {
		t.Errorf("err = %v, want the missing version refused first", err)
	}
}

// The cobra adapter reads whether --body was set from the command's flags.
func TestCobraBody_ReadsTheCommandsFlags(t *testing.T) {
	cmd, raw, _ := parsedCobra(t, "--body", `{"a":1}`)
	if body, err := input.CobraBody(cmd, raw, never(t)); err != nil || string(body) != `{"a":1}` {
		t.Errorf("CobraBody(--body) = %q, %v; want the bytes as given", body, err)
	}
	cmd, raw, _ = parsedCobra(t)
	if body, err := input.CobraBody(cmd, raw, built(fields{Code: "x"})); err != nil || string(body) != `{"code":"x","name":""}` {
		t.Errorf("CobraBody() = %q, %v; want what the flags build", body, err)
	}
}
