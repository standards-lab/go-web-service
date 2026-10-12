package input

import (
	"encoding/json"
	"errors"
	"fmt"

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
	if inv.Changed("body") {
		return []byte(raw), nil
	}
	v, err := fromFlags()
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
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
	flagged := inv.Changed("version")
	if !inv.Changed("body") {
		if !flagged {
			return nil, 0, errors.New("required flag --version not set")
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
