package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// timeout bounds every request a Client sends, whatever context it is given:
// a scenario that hangs on one call should fail with a reason rather than
// sit until the user interrupts it. A caller with a shorter bound passes it
// through the context.
const timeout = 30 * time.Second

// Client issues requests against one service through the same HTTP surface
// any client uses. It is the shape of go-web-sdk's webtest.Client with the
// testing.TB taken out: a scenario has no test to fail, so every call takes
// a context and returns an error.
type Client struct {
	base string
	http *http.Client
}

// NewClient binds a client to base, the service's URL. The transport holds
// one connection per host, for the reason webtest.NewClient gives: the
// default transport can dial a second connection while an idle one is being
// returned and leave it unused, and the server's graceful shutdown waits for
// a connection that never sent a request.
func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: newHTTPClient(timeout)}
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 1
	transport.MaxIdleConnsPerHost = 1
	return &http.Client{Timeout: timeout, Transport: transport}
}

// Header is one request header.
type Header struct{ Name, Value string }

// Do sends method path with body encoded as JSON when it is not nil (a
// []byte or string body is sent as is), and reads the response whole. An
// empty []byte or string is no body: nothing is sent and no Content-Type is
// set, the same as nil, so a caller whose body is optional passes what it
// has without converting an empty value to nil first. The error names the
// request that failed.
func (c *Client) Do(ctx context.Context, method, path string, body any, headers ...Header) (*Response, error) {
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		if len(b) > 0 {
			reader = bytes.NewReader(b)
		}
	case string:
		if len(b) > 0 {
			reader = strings.NewReader(b)
		}
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			return nil, fmt.Errorf("%s %s: encode body: %w", method, path, err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("%s %s: build request: %w", method, path, err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range headers {
		req.Header.Set(h.Name, h.Value)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: raw}, nil
}

// Get, Post, Put, and Delete are Do with the method named.
func (c *Client) Get(ctx context.Context, path string, headers ...Header) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, nil, headers...)
}

func (c *Client) Post(ctx context.Context, path string, body any, headers ...Header) (*Response, error) {
	return c.Do(ctx, http.MethodPost, path, body, headers...)
}

func (c *Client) Put(ctx context.Context, path string, body any, headers ...Header) (*Response, error) {
	return c.Do(ctx, http.MethodPut, path, body, headers...)
}

func (c *Client) Delete(ctx context.Context, path string, headers ...Header) (*Response, error) {
	return c.Do(ctx, http.MethodDelete, path, nil, headers...)
}

// Expect returns nil when the response carries status, and otherwise an
// error naming the status it carries and quoting the body, which for a
// problem response is the reason.
func (r *Response) Expect(status int) error {
	if r.Status == status {
		return nil
	}
	return fmt.Errorf("status = %d %s, want %d; body: %s", r.Status, http.StatusText(r.Status), status, r.Body)
}

// JSON decodes the body into v.
func (r *Response) JSON(v any) error {
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("decode body: %w; body: %s", err, r.Body)
	}
	return nil
}
