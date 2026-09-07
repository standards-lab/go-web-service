# go-web-service

The reference web service of Go Elemental, the Standards Lab organization's Go implementation of
the Elemental Architecture: a production web service composed from go-core, go-web-sdk, go-database, and sqlate on the
go-web-sdk-template baseline, on one declared stack (Postgres). Managed with the marathon
workflow; start from `context/README.md`.

## Repository specifics

- **Module layout.** One module at the root, `github.com/standards-lab/go-web-service`, with
  one binary, `cmd/server`, composing on go-core's `process` package for the pre-infrastructure
  main sequence. The composition root is `internal/app`, one file per layer (infrastructure,
  admin, domain, reactors) with `routes.go` the list of mounts. The base layers are root-level
  packages: `data` (the database infrastructure as the domains see it: the session with the
  pattern catalog and the statements registry, the migration set, the application's patterns,
  the seeder, the lock-name registry, the directives lowering, and the shared status matcher),
  `domain/<layer>` (one package per domain, its SQL under `statements/`), `admin/<service>` (the
  HTTP half of an admin service, mounted under `/admin`), and `sdk` (promotion candidates
  staged for the libraries).
- **Dependencies.** go-core, go-web-sdk, go-database with go-database/postgres, and sqlate with
  sqlate/postgres at pinned releases, on Go 1.27; sqlate's `sqlint` is a `tool` directive. The
  pins are the committed steady state; a gitignored local `go.work` serves sibling development.
- **SQL.** Every statement is an authored `.sql` file with a `--|` tier header, compiled by
  sqlate against the catalog at construction and verified against the live schema at startup.
  Schema migrations live under `data/migrations` and apply at startup through the database admin
  service; `sqlint.toml` at the root names the sources and roles the lint checks.
- **Stack.** Postgres is the declared SQL engine, run locally through `compose.yml`. A provider
  variant is never a switch inside this service; it would be a separate focused reference.
- **Documented layers.** The documented layer is the unit of change — each capability lands
  complete before the next begins (`context/design/documented-layers.md`). The service is
  versionless until its first release: no tags yet; `CHANGELOG.md` accumulates under
  `[Unreleased]` until the first cut, which `release.yml` turns into a GitHub release.
- **Tests.** Two tiers. The unit tier is hermetic: no live database, a package that runs SQL
  tests over sqlate's scripted driver (`sqlate/sqltest`), and `internal/config/configtest` as
  the single source of valid test configuration. The integration tier is the root `integration`
  package: an untagged harness over the SDKs' toolkit (go-core's `process/processtest` runs the
  built `cmd/server` as a subprocess and relays the database; go-web-sdk's `webtest` drives it
  through its API), adding only the service's configuration and admin-mount state control, and
  the suite under the `integration` build tag, run by `mise run integration` against the
  compose stack and by CI on merge to main. The runtime carries nothing for the tests' sake.
- **Public repo.** This repository is public on GitHub.
