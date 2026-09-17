package organization

import (
	"context"
	"net/url"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Organizations is the route domain/organization's group is mounted at:
// /organizations inside internal/app's /api mount.
const Organizations = "/api/organizations"

// Client sends one request per endpoint of the organization API through an
// httpx.Client. Its methods send and return the exchange as observed; they
// do not check the status or decode the body, which is the command's job
// through output.
type Client struct {
	http *httpx.Client
}

// NewClient wraps c, an httpx.Client bound to the service's base URL. The
// composition root constructs the httpx.Client; this package only calls it.
func NewClient(c *httpx.Client) *Client {
	return &Client{http: c}
}

// List sends GET Organizations with query, name=value pairs joined by
// httpx.RawQuery so a filter name like code[like] reaches the server with
// its brackets as written. No pairs sends no query component.
func (c *Client) List(ctx context.Context, query ...[2]string) (*httpx.Response, error) {
	path := Organizations
	if len(query) > 0 {
		path += "?" + httpx.RawQuery(query...)
	}
	return c.http.Get(ctx, path)
}

// Find sends GET Organizations/{id}.
func (c *Client) Find(ctx context.Context, id string) (*httpx.Response, error) {
	return c.http.Get(ctx, Organizations+"/"+url.PathEscape(id))
}

// FindByPath sends GET Organizations/path/{path}. The service reads the
// remainder after /path/ and prefixes a slash to make the hierarchy path, so
// a leading slash on path is dropped here rather than doubled.
func (c *Client) FindByPath(ctx context.Context, path string) (*httpx.Response, error) {
	return c.http.Get(ctx, Organizations+"/path/"+strings.TrimLeft(path, "/"))
}

// Create sends POST Organizations with body as the request body, verbatim.
func (c *Client) Create(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Organizations, body)
}

// Edit sends PUT Organizations/{id} with body verbatim and If-Match at
// version.
func (c *Client) Edit(ctx context.Context, id string, version int64, body []byte) (*httpx.Response, error) {
	return c.http.Put(ctx, Organizations+"/"+url.PathEscape(id), body, httpx.IfMatch(version))
}

// Transfer sends POST Organizations/{id}/transfer with body verbatim and
// If-Match at version.
func (c *Client) Transfer(ctx context.Context, id string, version int64, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Organizations+"/"+url.PathEscape(id)+"/transfer", body, httpx.IfMatch(version))
}

// Delete sends DELETE Organizations/{id} with If-Match at version and no
// body.
func (c *Client) Delete(ctx context.Context, id string, version int64) (*httpx.Response, error) {
	return c.http.Delete(ctx, Organizations+"/"+url.PathEscape(id), httpx.IfMatch(version))
}
