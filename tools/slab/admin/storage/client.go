package storage

import (
	"context"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Storage is the route admin/storage's group is mounted at: /storage inside
// internal/app's /admin mount.
const Storage = "/admin/storage"

// Client sends one request per endpoint of the storage admin API through an
// httpx.Client. Its methods send and return the exchange as observed; they
// do not check the status or decode the body, which is the command's job
// through output.
type Client struct {
	http *httpx.Client
}

// NewClient wraps c, an httpx.Client bound to the service's base URL.
func NewClient(c *httpx.Client) *Client {
	return &Client{http: c}
}

// Diagnostics sends GET Storage/diagnostics.
func (c *Client) Diagnostics(ctx context.Context) (*httpx.Response, error) {
	return c.http.Get(ctx, Storage+"/diagnostics")
}

// Container sends POST Storage/container with no body.
func (c *Client) Container(ctx context.Context) (*httpx.Response, error) {
	return c.http.Post(ctx, Storage+"/container", nil)
}
