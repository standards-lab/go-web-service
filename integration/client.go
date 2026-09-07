package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

// Client issues requests against one service through the same HTTP surface
// any client uses, and its Response carries the status, headers, and body
// for the assertions a test makes.
type Client struct {
	base string
	http *http.Client
}

// NewClient binds a client to base, the service's URL.
func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: Failsafe}}
}

// Header is one request header.
type Header struct{ Name, Value string }

// IfMatch is the version precondition a guarded command takes, quoted as
// the SDK requires.
func IfMatch(version int64) Header {
	return Header{"If-Match", fmt.Sprintf("%q", fmt.Sprint(version))}
}

// Response is one response, read whole.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends method path with body encoded as JSON when it is not nil (a
// []byte or string body is sent as is), and reads the response whole. A
// transport failure fails the test.
func (c *Client) Do(t testing.TB, method, path string, body any, headers ...Header) *Response {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	case string:
		reader = strings.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range headers {
		req.Header.Set(h.Name, h.Value)
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: raw}
}

// Get, Post, Put, and Delete are Do with the method named.
func (c *Client) Get(t testing.TB, path string, headers ...Header) *Response {
	t.Helper()
	return c.Do(t, http.MethodGet, path, nil, headers...)
}

func (c *Client) Post(t testing.TB, path string, body any, headers ...Header) *Response {
	t.Helper()
	return c.Do(t, http.MethodPost, path, body, headers...)
}

func (c *Client) Put(t testing.TB, path string, body any, headers ...Header) *Response {
	t.Helper()
	return c.Do(t, http.MethodPut, path, body, headers...)
}

func (c *Client) Delete(t testing.TB, path string, headers ...Header) *Response {
	t.Helper()
	return c.Do(t, http.MethodDelete, path, nil, headers...)
}

// Expect asserts the status and returns the response for further reads.
func (r *Response) Expect(t testing.TB, status int) *Response {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d; body: %s", r.Status, status, r.Body)
	}
	return r
}

// JSON decodes the body into v, failing the test on a decode error.
func (r *Response) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode body: %v; body: %s", err, r.Body)
	}
}

// Problem asserts the response is an RFC 9457 problem with the given
// status, as the SDK writes one, and returns it.
func (r *Response) Problem(t testing.TB, status int) web.Problem {
	t.Helper()
	r.Expect(t, status)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, web.ProblemMediaType) {
		t.Fatalf("Content-Type = %q, want %s; body: %s", ct, web.ProblemMediaType, r.Body)
	}
	var p web.Problem
	r.JSON(t, &p)
	if p.Status != status {
		t.Fatalf("problem status = %d, want %d; body: %s", p.Status, status, r.Body)
	}
	return p
}

// Decode is Expect then JSON, for the common read.
func Decode[T any](t testing.TB, r *Response, status int) T {
	t.Helper()
	var v T
	r.Expect(t, status).JSON(t, &v)
	return v
}
