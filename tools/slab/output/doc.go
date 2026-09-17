// Package output renders the result of a direct command, one that sends a
// single request and prints what came back with no narration: the org and
// admin database command families. Both families render identically, so the
// rendering belongs to neither; it lives here, beside httpx, for both to
// import.
//
// Response writes a success to the command's stdout: the body pretty-printed
// when there is one, or the status line (204 No Content) when there is not,
// so a command is never silent. Error writes a failure to the command's
// stderr: the members of a problem document when the error is or wraps a
// web.Problem, or the error's message otherwise. Expect turns a response
// with the wrong status into that error, decoding the RFC 9457 problem
// document the service answers with. The exit code is the composition
// root's concern, not this package's: a command returns the error, and the
// root renders it and exits non-zero.
//
// FixedCommand builds the whole subcommand for an endpoint that takes no
// input, over a func(context.Context) (*httpx.Response, error): the shape a
// domain client's no-input method has once its receiver is bound, so any
// command family uses it without this package naming that family's client.
package output
