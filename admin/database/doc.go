// Package database is the database admin domain: the HTTP half of the
// database admin service, a route group under the admin mount over
// go-database's admin.Service, whose operations are the library functions
// startup runs. The admin mount is the /admin route group, the
// administrative counterpart of /api; an admin domain is a package under
// admin/ named for the infrastructure service it administers. The mount
// serves on the API listener until the management listener gives it its
// own, authenticated.
//
// The package's API:
//
//   - [Routes]: the route group, its schema-changing verbs holding a
//     [SchemaGate] exclusively.
//   - [Steps], [Force], [State], and [Reset]: the verbs' request bodies.
//   - [ErrUnconfirmed]: a reset whose body does not confirm it, a 400.
package database
