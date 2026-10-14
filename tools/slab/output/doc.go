// Package output renders the result of a direct command, one that sends a
// single request and prints what came back with no narration: the org,
// docs, and admin command families. The families render identically, so the
// rendering belongs to none of them; it lives here, beside httpx, for each
// to import. A success renders through the style package, as a scenario's
// blocks do, so a direct command and a narrated one render alike. The exit code is not this package's concern: a command
// returns the error, and go-cli-sdk's dispatcher reports it and exits
// non-zero. A problem response's error is its [Report], which renders every
// member of the document, so the one report the dispatcher prints carries
// all of it.
//
// The package also renders the blocks a scenario on go-cli-sdk's runner
// shows under its prose: each is a [Render] the step hands to the
// reporter's Show, styled by the run's color decision as a direct
// command's result is.
//
// The package exports:
//
//   - [Output] and [New], the one value every output concern is configured
//     on, which renders a success, a status, an object, or a block a
//     command streams, such as each event a watch receives
//   - [Source], how a command on go-cli-sdk reaches its Output, and
//     [Source.Respond], which finishes a command's Run over one request
//   - [Expect], which turns a response with the wrong status into the error
//     a command returns
//   - [Report], the error of a problem response, the document rendered
//     member by member
//   - [Render], one block of a scenario's narration as go-cli-sdk's
//     scenario.Reporter.Show takes it, and the blocks a step shows
//     through it: [SQL] and [JSON], a statement and a document as
//     authored; [Table], name/value pairs; [Request] and [Response], the
//     two sides of an HTTP call; and [Trace], the pointer to a request's
//     trace in Grafana
package output
