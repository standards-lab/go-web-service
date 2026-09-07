// Package data is the service's database infrastructure as the domains
// see it: the session every statement runs through grouped with the
// pattern catalog the statements compile against, the dialect reachable
// through the session, and the content the admin service administers.
// Neither library may own the grouping: sqlate must not know the pool,
// go-database must not know patterns, and the composition root cannot,
// because the domains would import it. It is a root-level package, beside
// domain/ and admin/, because the domain packages import it and a
// root-level package never imports internal/*.
//
// The content is the schema (migrations/), the application's pattern
// namespace (patterns/), and the named states with their seed statements
// (seeds/, statements/), each behind a function the admin service
// triggers: Migrations, Patterns, and a Seeder. Beside them sit the three pieces
// every domain would otherwise copy: the advisory-lock name registry, the
// lowering from the web SDK's query to the library's directives, and the
// status matcher over the library's error vocabulary. Domains and the
// admin service are peers over this package; nothing here imports either.
package data
