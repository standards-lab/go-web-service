# Changelog

All notable changes to `github.com/standards-lab/go-web-service` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the service adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The service is unreleased; changes
accumulate under [Unreleased] until the first cut.

## [Unreleased]

### Added

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

- The organization domain runs on authored SQL: one `.sql` file per statement under
  `domain/organization/statements`, compiled by sqlate at construction and verified against the
  live schema at startup. Commands validate themselves on the entity types.
- Edit is `PUT /api/organizations/{id}`, full replacement; transfer requires the `parent_id`
  key, null meaning the root.
- The composition root is one file per layer under `internal/app`, as go-web-sdk-template
  v0.6.0 ships it.
- Pins: go-database v0.4.0 with postgres/v0.3.0, go-web-sdk v0.6.0, sqlate v0.1.0 with
  postgres/v0.1.1.

### Removed

- `cmd/db` and golang-migrate: migrations and seeding are library mechanisms the composition
  root triggers, and the admin mount exposes the former verbs.
- The `internal/infrastructure`, `internal/domain`, and `internal/reactors` packages.
- `sdk.IfMatch`, promoted to go-web-sdk v0.6.0 with the strict body decode.

[Unreleased]: https://github.com/standards-lab/go-web-service/commits/main
