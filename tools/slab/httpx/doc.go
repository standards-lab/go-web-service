// Package httpx is the HTTP client slab's scenarios call the service
// through. It holds everything about HTTP that is not specific to the
// service: the Client that sends a request, the Response the reporter
// renders, IfMatch, RawQuery, Problem, TraceID, and the Live probes a
// scenario's Need checks the service with.
//
// The Client treats an empty []byte or string body like nil: it sets no
// Content-Type and sends nothing, so a caller with an optional body passes
// what it has without converting it.
package httpx
