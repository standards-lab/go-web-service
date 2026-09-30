// Package sdk stages the service's library promotion candidates: request
// and lifecycle conventions proven here in running code before they move
// outward. The package is flat by design, a staging area meant to empty
// out, and each file is named for the library its contents are bound for.
//
// The package's API:
//
//   - [Command] (web.go, for go-web-sdk): a guarded command's three inputs,
//     the {id} path value, the If-Match version, and the strict body.
//   - [Reactor], built by [New] with its [Option] [Grace] (reactor.go, for
//     go-core): one [Source] of occurrences joined to one [Func] for the
//     process lifetime; [Every] and [Wake] (a [Waker]) are the sources, and
//     [Draining] tells a handler the drain has begun.
//   - [Gate] (gate.go, for go-core): the quiesce gate, a context-aware
//     readers-writer gate that prefers its exclusive side, which background
//     work holds shared and an operation that must pause it holds
//     exclusively.
package sdk
