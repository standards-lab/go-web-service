// Package sdk stages the service's library promotion candidates:
// conventions proven here in running code before they move outward. The
// package is flat by design, a staging area meant to empty out, and each
// file is named for the library its contents are bound for.
//
// The package's API:
//
//   - [Command] (web.go, for go-web-sdk): a guarded command's three inputs,
//     the {id} path value, the If-Match version, and the strict body.
//   - [WriteFile] (blobfs.go, for blobfs): blobfs's two-phase write with a
//     hook in the completion's transaction, where a domain emits the event
//     that reports the write.
//   - [EnsureFile] (blobfs.go, for blobfs): blobfs's retry-safe write with
//     WriteFile's hook, run only when the call completes the file; a file
//     found complete runs none.
//   - [RemoveFile] (blobfs.go, for blobfs): blobfs's two-phase delete with a
//     hook in the delete's first transaction, where a domain emits the
//     event that reports the delete; a retry of a delete begun already
//     skips the hook and finishes the purge.
package sdk
