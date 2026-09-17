// Package database is slab's client-side counterpart of the service's
// admin/database package: the database command family, one subcommand per
// endpoint the service's database admin service exposes under
// /admin/database. It is not an admin service itself. The service's package
// is the HTTP half of an admin service over an Infrastructure Service, the
// database; this package consumes that HTTP surface and administers nothing,
// so the one-to-one name across the two trees is for navigation. It is kept
// a sibling of slab's domain tree, not a member of it, the way the service
// keeps admin/ and domain/ as separate root-level trees.
//
// The package has one file per role. entities.go restates the request bodies
// the service defines, with the same json tags, so a server-side rename
// breaks slab rather than being followed silently; the response types are not
// restated, because every command prints the response as it came and decodes
// none of them. client.go names the route and sends one request per endpoint
// through httpx; it is the only file here that imports httpx. commands.go
// builds the database command, its schema subcommand, and the twelve leaf
// subcommands over a Client, each resolving its input from field flags or a
// verbatim --body and rendering what came back through output.
package database
