// Package sdk stages the service's library promotion candidates: request
// conventions proven here in running code before they move outward. The
// package is flat by design, a staging area meant to empty out, and each
// file is named for the library its contents are bound for.
//
// The package's API:
//
//   - [Command] (web.go, for go-web-sdk): a guarded command's three inputs,
//     the {id} path value, the If-Match version, and the strict body.
package sdk
