// Package httpx is the HTTP client the scenarios call the service through:
// the Client that sends a request, the Response the reporter renders, and
// the Live probe a scenario's Need checks the service with.
package httpx

import "net/http"

// Response is one HTTP exchange as a scenario observed it: the status, the
// headers, and the body read to completion.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}
