// Package document is slab's client-side counterpart of the service's
// domain/document package: the docs command family, one subcommand per
// endpoint the service's Domain Service exposes under /api/documents. Like
// its organization sibling it is not a Domain Service itself; it consumes
// the HTTP surface, and the one-to-one name across the two trees is for
// navigation.
//
// Every route sits under an organization's id, and a directory's id may be
// the root alias, so each command takes the organization first and a
// directory as an id or root. The commands group by what they address: dirs
// for the directories (create, get, list, move, delete) and files for the
// files (list, put, show, get, move, delete).
//
// The package has one file per role. entities.go restates the wire types the
// service defines, with the same json tags, so a server-side rename breaks
// slab rather than being followed silently. client.go names the routes and
// sends one request per endpoint through httpx; it is the only file here
// that imports httpx. commands.go builds the docs command and its two
// groups over a Client, each leaf resolving its input from field flags or a
// verbatim --body and rendering what came back through output.
package document
