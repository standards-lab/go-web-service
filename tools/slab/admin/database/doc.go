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
// The package exports:
//
//   - [Database], the route the admin service is mounted at
//   - [Client] and [NewClient], one request per endpoint
//   - [Down], [Force], [Reset], [State], and [Steps], the request bodies
//   - [Commands], which builds the database command on go-cli-sdk over the
//     client node
//
// The package has one file per role. entities.go restates the request bodies
// the service defines, with the same json tags, so a server-side rename
// breaks slab rather than being followed silently; the response types are not
// restated, because every command prints the response as it came and decodes
// none of them. client.go names the route and sends one request per endpoint
// through httpx; it is the only file here that imports httpx. commands.go
// builds the database command, its schema subcommand, and the twelve leaf
// subcommands over a Client, each resolving its input from field flags or a
// verbatim --body and rendering what came back through output. A leaf
// refuses an input the service would refuse, so the request is not sent:
// an unset --set, --steps on steps, or --version on force, where 0 is a
// meaningful version (an empty history), and an unset --state on state.
// What the service decides stays the service's: down omits an unset
// --steps and leaves the default to it, seed sends no body without
// --state so it applies its configured set, and state sends --confirm as
// given, the refusal of an unconfirmed reset being the service's to state.
package database
