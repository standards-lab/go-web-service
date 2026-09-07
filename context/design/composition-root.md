# The composition root

How this service composes its subsystems. The pattern is owned by go-web-sdk-template, the
composition root as one file per layer on the staged lifecycle coordinator, documented on the
standard's landing-zone pages, and this service consumes it as template/v0.6.0 ships it,
extending it with the database. Settled in the relay session that created the repository
(2026-08-24), reshaped onto the file-per-layer root and the database admin service at
`v1.data.sql.integration.service` (2026-09-06), proven by the running server.

## Layout

- `cmd/server` is the process entrypoint and nothing else: `main.go` composes its run function
  from go-core's `process` package (the signal-derived root context, pre-logger failure
  reporting) and hands off to the composition root. It is the module's one binary.
- `internal/app` is the composition root, one file per layer, so its table of contents is the
  architecture's layer list. `infrastructure.go` constructs the shared subsystems, the logger
  and the database, with provider selection (postgres) at the one place a provider is imported;
  it wraps the pool in the sqlate session with the engine's dialect and builds the pattern
  catalog once. `admin.go` constructs the admin services over the infrastructure and owns the
  `/admin` mount. `domain.go` constructs the domain services and owns the `/api` mount.
  `reactors.go` is the placeholder for the event-driven entry points. `routes.go` is the list of
  mounts and `middleware.go` the router-level stack. `app.go` assembles them and registers the
  server as the coordinator's root-stage service; `RegisterHealth` queries the coordinator live,
  so every registered check reaches `/readyz` with no route changes.
- The base layers are root-level packages: `data`, the database infrastructure as the domains
  see it; `domain/<layer>`, one package per domain; `admin/<service>`, the HTTP half of an admin
  service. They sit at the root because the domain packages import `data`, and the
  topology-and-naming principle forbids a root-level package importing `internal/*`. The layer
  files hand each package the fields it needs as constructor parameters, never the
  `Infrastructure` struct.
- Construction performs no I/O. Each lifecycle-bearing subsystem registers on the coordinator
  where it is built, so a subsystem cannot be constructed yet missing from startup, teardown, or
  the probe.

## Ordering

The staged coordinator owns ordering natively: numbered stages start ascending with the root
stage last, and the drain unwinds in reverse, the HTTP server draining before the database
beneath it closes. The stages are the startup contract:

- Stage 0, the pool: connects and reports live readiness.
- Stage 1, the schema: the database admin service verifies the migration history, applies a
  pending set under the advisory lock and verifies again, verifies the `data` package's
  statements against the migrated schema, and seeds when the environment enables it. A state it
  cannot correct, a dirty row or a history the set does not carry, fails startup; an operator
  resolves it through the admin mount's verbs.
- Stage 2, the domains: each verifies its own statements and read contract against the schema,
  so a renamed column fails startup naming the statement rather than the first request.
- The root stage, the server.

Startup within a stage is concurrent; a failed stage fails startup, and readiness never flips.

## The unit-test contract

The suite runs with no live database. `internal/app` proves the cold start and the startup
contract: construction performs no I/O, a dead database fails `Run` at stage 0 to exit 1
without a ready record, and the coordinator is single-use. Every package that runs SQL proves
its wiring over sqlate's scripted driver (`sqlate/sqltest`): the compiled text, the bound
arguments, the transaction shape, and the rejection paths. `internal/config/configtest` is the
single source of valid test configuration. The root `integration` package is the integration
tier: its harness runs the built `cmd/server` as a subprocess and drives it through its
production seams, and its tagged suite proves the serve, probe, and drain path, the behaviors
that need a real engine, and the 503 on a database outage, against the compose definition as
its own project. The README's serve, probe, and drain step is the one check a composition-root
change is verified by hand with.
