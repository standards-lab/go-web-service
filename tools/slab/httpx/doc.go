// Package httpx is the HTTP client the scenarios call the service through:
// the Client that sends a request, the Response the reporter renders, and
// the Live probe a scenario's Need checks the service with.
//
// It holds everything about HTTP that is not specific to the service:
// IfMatch, RawQuery, Problem, TraceID, and the Live probes. An empty []byte
// or string body is treated like nil, with no Content-Type and nothing sent,
// so a caller with an optional body passes what it has unconverted.
package httpx
