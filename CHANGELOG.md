# Changelog

All notable changes to `github.com/standards-lab/go-web-service` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the service adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The service is unreleased; changes
accumulate under [Unreleased] until the first cut.

## [Unreleased]

### Added

- Named database states: one file per state under `data/seeds/`, keyed by table (`default`, the
  reference tree; `empty`, no rows). `GET /admin/database/states` lists them;
  `POST /admin/database/state` resets the database to one, every migration reverted, the set
  applied, the state's set seeded, and answers with the transition; `POST /admin/database/seed`
  takes an optional `{"state": "…"}` to apply a named set over what is there. `mise run
  db-state <state>` runs the reset against the local service.
- The integration tier: the root `integration` package, a harness that runs the built service as
  a subprocess against the compose stack and a `//go:build integration` suite asserting the
  lifecycle, the organization API, the admin mount, and the 503 on a database outage through
  the API; `mise run integration`; the CI job on merge to main and `workflow_dispatch`. The
  harness's runner, forwarder, and client then promoted to go-core v0.4.0
  (`process/processtest`) and go-web-sdk v0.7.0 (`webtest`); the package keeps the service's
  configuration and its admin-mount state control over them.
- The `data` package: the database infrastructure as the domains see it. The session grouped
  with the pattern catalog and the statements registry, the migration set under
  `data/migrations`, the application's pattern namespace (`app.identity`), the seeder behind the
  admin service, the advisory-lock name registry with `Database.Lock`, the lowering from the web
  SDK's query to the library's directives, and the shared status matcher.
- The database admin service, go-database's `admin` package, mounted under `/admin/database`:
  diagnostics, the schema status, the pattern catalog, the statements inventory, the schema
  verbs, and seed. Startup verifies the schema, applies pending migrations, and seeds when
  `admin.seed` (`APP_ADMIN_SEED`) is on.
- The `admin` configuration block with the seed switch, off by default and on in the `local`
  overlay.
- Filter operators on the collection read, `field[op]=value`, and membership from a repeated
  parameter; a malformed filter value answers 400.
- `sqlint.toml` and the `sqlint` tool: `mise run lint` and CI lint the SQL files.

### Changed

- `admin.seed` (`APP_ADMIN_SEED`) names the state whose set applies at startup and on a
  bodyless seed, the way a deployment initializes its data; empty names none. The `local`
  overlay names `default`.
- The integration harness's `Reset` is one call to the state operation, and its `Options.Seed`
  is a state name.
- The organization domain runs on authored SQL: one `.sql` file per statement under
  `domain/organization/statements`, compiled by sqlate at construction and verified against the
  live schema at startup. Commands validate themselves on the entity types.
- Edit is `PUT /api/organizations/{id}`, full replacement; transfer requires the `parent_id`
  key, null meaning the root.
- The composition root is one file per layer under `internal/app`, as go-web-sdk-template
  v0.6.0 ships it.
- Pins: go-core v0.4.1, go-database v0.5.0 with postgres/v0.3.0, go-web-sdk v0.7.0, sqlate
  v0.1.1 with postgres/v0.1.1.

### Removed

- `cmd/db` and golang-migrate: migrations and seeding are library mechanisms the composition
  root triggers, and the admin mount exposes the former verbs.
- The `internal/infrastructure`, `internal/domain`, and `internal/reactors` packages.
- `sdk.IfMatch`, promoted to go-web-sdk v0.6.0 with the strict body decode.

[Unreleased]: https://github.com/standards-lab/go-web-service/commits/main
