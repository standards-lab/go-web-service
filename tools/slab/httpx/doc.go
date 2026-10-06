// Package httpx is the HTTP client slab's scenarios call the service
// through. It holds everything about HTTP that is not specific to the
// service.
//
// The package exports:
//
//   - [Client] and [NewClient], which send a request to the service
//   - [Response], one exchange as observed, which the reporter renders
//   - [Header] and [IfMatch], a request header and the precondition one
//   - [RawQuery], which joins query pairs as the printed request shows them
//   - [Live] and [GrafanaLive], the probes a scenario's Need checks with
package httpx
