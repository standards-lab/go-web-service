package database

import (
	"context"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Database is the route admin/database's group is mounted at: /database
// inside internal/app's /admin mount.
const Database = "/admin/database"

// Client sends one request per endpoint of the database admin API through
// an httpx.Client. Its methods send and return the exchange as observed;
// they do not check the status or decode the body, which is the command's
// job through output.
type Client struct {
	http *httpx.Client
}

// NewClient wraps c, an httpx.Client bound to the service's base URL. The
// composition root constructs the httpx.Client; this package only calls it.
func NewClient(c *httpx.Client) *Client {
	return &Client{http: c}
}

// Status sends GET Database/schema.
func (c *Client) Status(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Database+"/schema")
}

// Verify sends POST Database/schema/verify with no body.
func (c *Client) Verify(ctx context.Context) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/schema/verify", nil)
}

// Up sends POST Database/schema/up with no body.
func (c *Client) Up(ctx context.Context) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/schema/up", nil)
}

// Down sends POST Database/schema/down with body verbatim, or with no body
// when body is empty (httpx.Client.Do sends nothing for an empty []byte),
// which the service reads as one step.
func (c *Client) Down(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/schema/down", body)
}

// Steps sends POST Database/schema/steps with body verbatim.
func (c *Client) Steps(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/schema/steps", body)
}

// Force sends POST Database/schema/force with body verbatim.
func (c *Client) Force(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/schema/force", body)
}

// Seed sends POST Database/seed with body verbatim, or with no body when
// body is empty (httpx.Client.Do sends nothing for an empty []byte), which
// the service reads as its configured set.
func (c *Client) Seed(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/seed", body)
}

// Reset sends POST Database/state with body verbatim.
func (c *Client) Reset(ctx context.Context, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, Database+"/state", body)
}

// Diagnose sends GET Database/diagnostics.
func (c *Client) Diagnose(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Database+"/diagnostics")
}

// Catalog sends GET Database/patterns.
func (c *Client) Catalog(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Database+"/patterns")
}

// Statements sends GET Database/statements.
func (c *Client) Statements(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Database+"/statements")
}

// States sends GET Database/states.
func (c *Client) States(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Database+"/states")
}
