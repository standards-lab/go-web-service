// Package input resolves the request body of a direct command, one that
// sends a single request and prints what came back: the org, docs, and
// admin database command families. A body-sending command takes its input
// either way, a set of field flags the command binds, or a single
// --body <json> escape hatch sent verbatim; cobra's mutual exclusion keeps
// the two apart before any function here runs. The families resolve
// identically, so the resolution belongs to none of them; it lives here,
// beside output, for each to import. Neither body function touches the
// request; a command passes what it returns to its client.
//
// The package exports:
//
//   - [Body], the body of an unguarded command
//   - [GuardedBody], the body of a command under a precondition header, and
//     the version its If-Match carries
//   - [ReadFlags], the read grammar a collection read's command binds
//   - [DefaultPageSize], the page size the service gives a read that names
//     none
package input
