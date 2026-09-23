package httpx

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"

	"github.com/standards-lab/go-web-sdk"
)

// Response is one HTTP exchange as a scenario observed it: the status, the
// headers, and the body read to completion.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// traceIDPattern is the shape of a W3C trace id as the service renders it:
// 16 bytes, hex-encoded, lowercase.
var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TraceID returns the request's trace id, which the service echoes in the
// X-Request-Id header, or an error when the header is absent or does not
// have a trace id's shape (32 lowercase hex characters).
func (r *Response) TraceID() (string, error) {
	id := r.Header.Get("X-Request-Id")
	if id == "" {
		return "", errors.New("the response carries no X-Request-Id header")
	}
	if !traceIDPattern.MatchString(id) {
		return "", fmt.Errorf("X-Request-Id = %q, which is not a trace id (32 lowercase hex characters)", id)
	}
	return id, nil
}

// Problem decodes the response as an RFC 9457 problem document answering
// with status: the response carries that status, its Content-Type is the
// problem media type, its body decodes as a web.Problem, and the document's
// own status member agrees with the response's.
func (r *Response) Problem(status int) (web.Problem, error) {
	if err := r.Expect(status); err != nil {
		return web.Problem{}, err
	}
	ct := r.Header.Get("Content-Type")
	media, _, err := mime.ParseMediaType(ct)
	if err != nil || media != web.ProblemMediaType {
		return web.Problem{}, fmt.Errorf("Content-Type = %q, want %s; body: %s", ct, web.ProblemMediaType, r.Body)
	}
	var p web.Problem
	if err := r.JSON(&p); err != nil {
		return web.Problem{}, err
	}
	if p.Status != status {
		return web.Problem{}, fmt.Errorf("the problem document says status %d, but the response is %d; body: %s", p.Status, status, r.Body)
	}
	return p, nil
}
