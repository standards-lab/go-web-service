// Package output renders the result of a direct command, one that sends a
// single request and prints what came back with no narration: the org,
// docs, and admin command families. The families render identically, so the
// rendering belongs to none of them; it lives here, beside httpx, for each
// to import. A success renders through the same style package the demo
// scenarios color their own output with, so a direct command and a narrated
// one render alike. The exit code is the composition root's concern, not
// this package's: a command returns the error, and the root renders it and
// exits non-zero.
//
// The package exports:
//
//   - [Output] and [New], the one value every output concern is configured
//     on, which renders a success, a status, an object, or a failure
//   - [Expect], which turns a response with the wrong status into the error
//     a command returns
package output
