// Package httpx is the HTTP client the scenarios call the service through.
// This stage defines only the Response type the reporter renders; the client
// that produces one arrives with the first scenario that needs it.
package httpx

import "net/http"

// Response is one HTTP exchange as a scenario observed it: the status, the
// headers, and the body read to completion.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}
