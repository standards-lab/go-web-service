// Package output renders the result of a direct command, one that sends a
// single request and prints what came back with no narration: the org and
// admin database command families. Both families render identically, so the
// rendering belongs to neither; it lives here, beside httpx, for both to
// import.
//
// Output is the one value every output concern is configured on: the stream
// a success goes to, the stream a failure goes to, and whether either is
// colored. The composition root constructs one with New and hands it to each
// command family, which renders through its methods and names no stream or
// style itself. The streams are fixed at New; the color decision is a
// function Output asks each time it renders, because --no-color is a
// persistent flag cobra parses during execution, after the tree is built,
// and a render always happens from a command's RunE, after that. Style
// returns the styling as it stands, for a command that renders something
// of its own.
//
// Response writes a success to stdout: the body pretty-printed and colored
// when there is one, or the status line (204 No Content) when there is not,
// so a command is never silent — through the same style package the demo
// scenarios color their own output with, so a direct command and a narrated
// one render alike. Error writes a failure to stderr: the members of a
// problem document when the error is or wraps a web.Problem, or the error's
// message otherwise. Expect turns a response with the wrong status into that
// error, decoding the RFC 9457 problem document the service answers with.
// The exit code is the composition root's concern, not this package's: a
// command returns the error, and the root renders it and exits non-zero.
//
// FixedCommand builds the whole subcommand for an endpoint that takes no
// input, over a func(context.Context) (*httpx.Response, error): the shape a
// domain client's no-input method has once its receiver is bound, so any
// command family uses it without this package naming that family's client.
package output
