// Package output renders the result of a direct command, one that sends a
// single request and prints what came back with no narration: the org,
// docs, and admin command families. The families render identically, so the
// rendering belongs to none of them; it lives here, beside httpx, for each
// to import. A success renders through the same style package the demo
// scenarios color their own output with, so a direct command and a narrated
// one render alike. The exit code is not this package's concern: a command
// returns the error, and go-cli-sdk's dispatcher reports it and exits
// non-zero. A problem response's error is its [Report], which renders every
// member of the document, so the one report the dispatcher prints carries
// all of it.
//
// The package exports:
//
//   - [Output] and [New], the one value every output concern is configured
//     on, which renders a success, a status, or an object
//   - [Source], how a command on go-cli-sdk reaches its Output, and
//     [Source.Respond], which finishes a command's Run over one request
//   - [Expect], which turns a response with the wrong status into the error
//     a command returns
//   - [Report], the error of a problem response, the document rendered
//     member by member
package output
