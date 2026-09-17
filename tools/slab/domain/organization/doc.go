// Package organization is slab's client-side counterpart of the service's
// domain/organization package: the org command family, one subcommand per
// endpoint the service's Domain Service exposes under /api/organizations.
// It is not a Domain Service itself. The service's package presents the
// organization hierarchy as an HTTP API; this package consumes that API and
// presents nothing, so the one-to-one name across the two trees is for
// navigation, not a claim that slab has a domain of its own.
//
// The package has one file per role. entities.go restates the wire types the
// service defines, with the same json tags, so a server-side rename breaks
// slab rather than being followed silently. client.go names the routes and
// sends one request per endpoint through httpx; it is the only file here
// that imports httpx. commands.go builds the org command and its seven
// subcommands over a Client, each resolving its input from field flags or a
// verbatim --body and rendering what came back through output.
package organization
