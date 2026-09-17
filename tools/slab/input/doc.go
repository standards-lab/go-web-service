// Package input resolves the request body of a direct command, one that
// sends a single request and prints what came back: the org and admin
// database command families. A body-sending command takes its input either
// way, a set of field flags the command binds, or a single --body <json>
// escape hatch sent verbatim; cobra's mutual exclusion keeps the two apart
// before any function here runs. Both families resolve identically, so the
// resolution belongs to neither; it lives here, beside output, for both to
// import.
//
// Body is the unguarded case: the --body bytes as given, or the value the
// command builds from its flags, marshaled. GuardedBody is the case under a
// precondition header: the same body, and the version its If-Match carries,
// which the --version flag supplies or, on the --body path, a top-level
// "version" number in the JSON stands in for. Neither function touches the
// request; a command passes what they return to its client.
package input
