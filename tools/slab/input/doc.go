// Package input resolves the request input of a direct command, one that
// sends a single request and prints what came back: the org, docs, and
// admin database command families. A body-sending command takes its input
// either way, a set of field flags the command binds, or a single
// --body <json> escape hatch sent verbatim; the command's exclusive flag
// groups keep the two apart before any function here runs. The families
// resolve identically, so the resolution belongs to none of them; it lives
// here, beside output, for each to import. Neither body function touches
// the request; a command passes what it returns to its client.
//
// The functions read from go-cli-sdk's Invocation whether each flag was
// given, and a command calls them from its Validate, so input they refuse
// is a usage error and nothing is built. The families still on cobra call
// the Cobra variants, which read the same from a cobra command's flags and
// go when the last of those families moves onto go-cli-sdk.
//
// The package exports:
//
//   - [Body], the body of an unguarded command, and [CobraBody]
//   - [GuardedBody], the body of a command under a precondition header, and
//     the version its If-Match carries, and [CobraGuardedBody]
//   - [ReadFlags], the read grammar a collection read's command binds, with
//     [ReadFlags.Bind] and [ReadFlags.Query], and [ReadFlags.BindCobra] and
//     [ReadFlags.QueryCobra]
//   - [DefaultPageSize], the page size the service gives a read that names
//     none
package input
