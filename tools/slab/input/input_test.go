package input_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/input"
)

// fields is a stand-in for the wire type a command builds from its flags.
type fields struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// parsed is a command with the --body and --version flags every body-sending
// command binds, after parsing args, along with the flag values as the
// command's RunE would hold them.
func parsed(t *testing.T, args ...string) (cmd *cobra.Command, raw string, version int64) {
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
		cmd, got, _ := parsed(t, "--body", raw)
		body, err := input.Body(cmd, got, never(t))
		if err != nil {
			t.Fatalf("Body(--body %q) = %v", raw, err)
		}
		if string(body) != raw {
			t.Errorf("Body(--body %q) = %q, want the bytes as given", raw, body)
		}
	}
}

func TestBody_MarshalsWhatTheFlagsBuild(t *testing.T) {
	cmd, raw, _ := parsed(t)
	body, err := input.Body(cmd, raw, built(fields{Code: "x", Name: "y"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":"x","name":"y"}`; string(body) != want {
		t.Errorf("Body() = %s, want %s", body, want)
	}
}

func TestBody_ReturnsTheFlagsPathError(t *testing.T) {
	cmd, raw, _ := parsed(t)
	want := errors.New("--code and --name are required unless --body is given")
	body, err := input.Body(cmd, raw, func() (any, error) { return nil, want })
	if err != want {
		t.Errorf("err = %v, want fromFlags's error as is", err)
	}
	if body != nil {
		t.Errorf("body = %q, want nil", body)
	}
}

func TestBody_ReturnsTheMarshalError(t *testing.T) {
	cmd, raw, _ := parsed(t)
	if _, err := input.Body(cmd, raw, built(make(chan int))); err == nil {
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
			cmd, raw, version := parsed(t, tc.args...)
			fromFlags := never(t)
			if !cmd.Flags().Changed("body") {
				fromFlags = built(fields{Code: "x", Name: "y"})
			}
			body, ifMatch, err := input.GuardedBody(cmd, raw, version, fromFlags)
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
		"flags path":         {nil, `required flag(s) "version" not set`},
		"body without key":   {[]string{"--body", `{"code":"x","name":"y"}`}, `--version is required, as the flag or as a top-level "version" number in --body`},
		"body with a string": {[]string{"--body", `{"code":"x","version":"4"}`}, `--body: "version" is not an integer: "4"`},
		"body with a float":  {[]string{"--body", `{"version":1.5}`}, `--body: "version" is not an integer: 1.5`},
		"body not an object": {[]string{"--body", `[1]`}, "--body is not a JSON object"},
		"body not JSON":      {[]string{"--body", `nope`}, "--body is not a JSON object"},
	} {
		cmd, raw, version := parsed(t, tc.args...)
		body, ifMatch, err := input.GuardedBody(cmd, raw, version, built(fields{}))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if body != nil || ifMatch != 0 {
			t.Errorf("%s: returned body %q and version %d alongside the error", name, body, ifMatch)
		}
	}
}

func TestGuardedBody_ReturnsTheFlagsPathError(t *testing.T) {
	cmd, raw, version := parsed(t, "--version", "1")
	want := errors.New("--parent-id is required unless --body is given")
	body, ifMatch, err := input.GuardedBody(cmd, raw, version, func() (any, error) { return nil, want })
	if err != want {
		t.Errorf("err = %v, want fromFlags's error as is", err)
	}
	if body != nil || ifMatch != 0 {
		t.Errorf("returned body %q and version %d alongside the error", body, ifMatch)
	}
}

func TestGuardedBody_ChecksTheVersionBeforeBuildingFromFlags(t *testing.T) {
	cmd, raw, version := parsed(t)
	_, _, err := input.GuardedBody(cmd, raw, version, never(t))
	if err == nil || !strings.Contains(err.Error(), `required flag(s) "version" not set`) {
		t.Errorf("err = %v, want the missing version refused first", err)
	}
}
