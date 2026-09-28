package document

import (
	"context"
	"fmt"
	"net/url"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Documents is the route domain/document's group is mounted at: /documents
// inside internal/app's /api mount. Every route below it starts with the
// organization's id.
const Documents = "/api/documents"

// Client sends one request per endpoint of the document API through an
// httpx.Client. Its methods send and return the exchange as observed; they
// do not check the status or decode the body, which is the command's job
// through output. Every path segment a caller supplies is escaped with
// url.PathEscape, a file's name on upload included.
type Client struct {
	http *httpx.Client
}

// NewClient wraps c, an httpx.Client bound to the service's base URL. The
// composition root constructs the httpx.Client; this package only calls it.
func NewClient(c *httpx.Client) *Client {
	return &Client{http: c}
}

// directories is Documents/{org}/directories.
func directories(org string) string {
	return Documents + "/" + url.PathEscape(org) + "/directories"
}

// directory is Documents/{org}/directories/{id}, id a directory id or the
// root alias.
func directory(org, id string) string {
	return directories(org) + "/" + url.PathEscape(id)
}

// file is Documents/{org}/files/{id}.
func file(org, id string) string {
	return Documents + "/" + url.PathEscape(org) + "/files/" + url.PathEscape(id)
}

// withQuery appends query, name=value pairs joined by httpx.RawQuery, to
// path; no pairs appends nothing.
func withQuery(path string, query [][2]string) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + httpx.RawQuery(query...)
}

// CreateDirectory sends POST Documents/{org}/directories with body verbatim.
func (c *Client) CreateDirectory(ctx context.Context, org string, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, directories(org), body)
}

// Directory sends GET Documents/{org}/directories/{id}.
func (c *Client) Directory(ctx context.Context, org, id string) (*httpx.Response, error) {
	return c.http.Get(ctx, directory(org, id))
}

// ListDirectories sends GET Documents/{org}/directories/{id}/directories
// with query, the read grammar's pairs as List's on the organization client.
func (c *Client) ListDirectories(ctx context.Context, org, id string, query ...[2]string) (*httpx.Response, error) {
	return c.http.Get(ctx, withQuery(directory(org, id)+"/directories", query))
}

// ListFiles sends GET Documents/{org}/directories/{id}/files with query.
func (c *Client) ListFiles(ctx context.Context, org, id string, query ...[2]string) (*httpx.Response, error) {
	return c.http.Get(ctx, withQuery(directory(org, id)+"/files", query))
}

// MoveDirectory sends POST Documents/{org}/directories/{id}/move with body
// verbatim and If-Match at version.
func (c *Client) MoveDirectory(ctx context.Context, org, id string, version int64, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, directory(org, id)+"/move", body, httpx.IfMatch(version))
}

// DeleteDirectory sends DELETE Documents/{org}/directories/{id} with
// If-Match at version, with ?recursive=true when recursive is set, and no
// query otherwise, which the service reads as false.
func (c *Client) DeleteDirectory(ctx context.Context, org, id string, version int64, recursive bool) (*httpx.Response, error) {
	path := directory(org, id)
	if recursive {
		path += "?recursive=true"
	}
	return c.http.Delete(ctx, path, httpx.IfMatch(version))
}

// DirectoryAt sends GET location, a directory read's path as a recursive
// delete's Location gave it. An absolute URL is sent as its path and query,
// against the client's own base.
func (c *Client) DirectoryAt(ctx context.Context, location string) (*httpx.Response, error) {
	u, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("location %q: %w", location, err)
	}
	return c.http.Get(ctx, u.RequestURI())
}

// PutFile sends PUT Documents/{org}/directories/{dir}/files/{name} with body
// as the raw request body under contentType.
func (c *Client) PutFile(ctx context.Context, org, dir, name, contentType string, body []byte) (*httpx.Response, error) {
	return c.http.Put(ctx, directory(org, dir)+"/files/"+url.PathEscape(name), body, httpx.Header{Name: "Content-Type", Value: contentType})
}

// File sends GET Documents/{org}/files/{id}, the file's metadata.
func (c *Client) File(ctx context.Context, org, id string) (*httpx.Response, error) {
	return c.http.Get(ctx, file(org, id))
}

// Content sends GET Documents/{org}/files/{id}/content, with If-None-Match
// when etag is set.
func (c *Client) Content(ctx context.Context, org, id, etag string) (*httpx.Response, error) {
	var headers []httpx.Header
	if etag != "" {
		headers = append(headers, httpx.Header{Name: "If-None-Match", Value: etag})
	}
	return c.http.Get(ctx, file(org, id)+"/content", headers...)
}

// MoveFile sends POST Documents/{org}/files/{id}/move with body verbatim and
// If-Match at version.
func (c *Client) MoveFile(ctx context.Context, org, id string, version int64, body []byte) (*httpx.Response, error) {
	return c.http.Post(ctx, file(org, id)+"/move", body, httpx.IfMatch(version))
}

// DeleteFile sends DELETE Documents/{org}/files/{id} with If-Match at
// version.
func (c *Client) DeleteFile(ctx context.Context, org, id string, version int64) (*httpx.Response, error) {
	return c.http.Delete(ctx, file(org, id), httpx.IfMatch(version))
}
