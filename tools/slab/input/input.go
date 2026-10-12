package input

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-cli-sdk"
)

// Body resolves what an unguarded body-sending command sends: the --body
// flag's bytes as given when it was set, and otherwise the value fromFlags
// builds, marshaled. raw is the --body flag's value; inv is consulted only
// for whether the flag was set. fromFlags returns the error for a missing
// field flag, so that refusal happens before the request fires. A command
// calls it from its Validate, so the refusal is a usage error and nothing
// is built.
func Body(inv *cli.Invocation, raw string, fromFlags func() (any, error)) ([]byte, error) {
	return body(inv.Changed, raw, fromFlags)
}

// CobraBody is [Body] for a command still on cobra, which reads whether
// --body was set from cmd's flags. It goes when the last such family moves
// onto go-cli-sdk.
func CobraBody(cmd *cobra.Command, raw string, fromFlags func() (any, error)) ([]byte, error) {
	return body(cmd.Flags().Changed, raw, fromFlags)
}

// GuardedBody resolves what a guarded body-sending command sends: the body
// and the version its If-Match carries. raw and version are the --body and
// --version flags' values; inv is consulted for whether each was set. The
// --version flag wins when set. Otherwise, on the --body path, a top-level
// "version" number in the JSON supplies it, so a caller of the escape hatch
// does not spell the value twice. The key is stripped from the body either
// way, because the wire type a guarded route decodes does not carry it and
// the service decodes with unknown fields disallowed; a body without the
// key is sent untouched. With neither source, the command is refused before
// the request fires, in the words go-cli-sdk refuses a missing required
// flag with. A command calls it from its Validate, as it does [Body].
func GuardedBody(inv *cli.Invocation, raw string, version int64, fromFlags func() (any, error)) ([]byte, int64, error) {
	return guardedBody(inv.Changed, raw, version, fromFlags, errors.New("required flag --version not set"))
}

// CobraGuardedBody is [GuardedBody] for a command still on cobra: it reads
// whether each flag was set from cmd's flags, and refuses a missing version
// in cobra's words. It goes when the last such family moves onto
// go-cli-sdk.
func CobraGuardedBody(cmd *cobra.Command, raw string, version int64, fromFlags func() (any, error)) ([]byte, int64, error) {
	return guardedBody(cmd.Flags().Changed, raw, version, fromFlags, errors.New(`required flag(s) "version" not set`))
}

// body is [Body] over changed, which reports whether a flag was set.
func body(changed func(name string) bool, raw string, fromFlags func() (any, error)) ([]byte, error) {
	if changed("body") {
		return []byte(raw), nil
	}
	v, err := fromFlags()
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// guardedBody is [GuardedBody] over changed, which reports whether a flag
// was set, refusing a request with no version source with noVersion.
func guardedBody(changed func(name string) bool, raw string, version int64, fromFlags func() (any, error), noVersion error) ([]byte, int64, error) {
	flagged := changed("version")
	if !changed("body") {
		if !flagged {
			return nil, 0, noVersion
		}
		v, err := fromFlags()
		if err != nil {
			return nil, 0, err
		}
		body, err := json.Marshal(v)
		if err != nil {
			return nil, 0, err
		}
		return body, version, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, 0, fmt.Errorf("--body is not a JSON object: %w", err)
	}
	embedded, present := fields["version"]
	if !present {
		if !flagged {
			return nil, 0, errors.New(`--version is required, as the flag or as a top-level "version" number in --body`)
		}
		return []byte(raw), version, nil
	}
	delete(fields, "version")
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, 0, err
	}
	if flagged {
		return body, version, nil
	}
	if err := json.Unmarshal(embedded, &version); err != nil {
		return nil, 0, fmt.Errorf(`--body: "version" is not an integer: %s`, embedded)
	}
	return body, version, nil
}
