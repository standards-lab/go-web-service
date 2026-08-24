# The composition root

How this service composes its subsystems. The pattern is owned by go-web-sdk-template — the
four-layer composition root on the staged lifecycle coordinator, documented on the standard's
landing-zone pages — and this service consumes it as generated, extending it with the database.
Settled in the relay session that created the repository (2026-08-24), proven by the running
server; it supersedes the composite-shutdown-hook arrangement the predecessor recorded, which
go-core's staged coordinator replaced.

## Layout

- `cmd/server` is the process entrypoint and nothing else: `main.go` composes its run function
  from `internal/process` (the signal-derived root context, pre-logger failure reporting) and
  hands off to the composition root.
- `internal/app` is the composition root: `App` assembles infrastructure, the domain, and the
  reactors into a router and the coordinator, with `routes.go` and `middleware.go` as the build
  points. The server registers as the coordinator's root-stage service; `RegisterHealth` queries
  the coordinator live, so every registered check reaches `/readyz` with no route changes.
- `internal/infrastructure` constructs the shared subsystems — the logger (inert) and the
  database — with provider selection (postgres) in `New`. Construction performs no I/O. Each
  lifecycle-bearing subsystem registers on the coordinator where it is built: the database at
  stage 0, ahead of the root stage, with its readiness check — so a subsystem cannot be
  constructed yet missing from startup, teardown, or the probe.
- `internal/domain` and `internal/reactors` are the template's domain and reactor layers, still
  empty; the data layer's domain services land in them.
- `internal/process` holds the pre-infrastructure main-sequence parts both binaries share; the
  exit-code convention cannot drift between them.
- `cmd/db` reuses the construction with no coordinator: `withInfrastructure` passes nil —
  `New` then registers nothing — and drives the database's `Start` and deferred `Shutdown`
  directly, inheriting the configured logger, pool settings, and provider.

## Ordering

The staged coordinator owns ordering natively: numbered stages start ascending with the root
stage last, and the drain unwinds in reverse — the HTTP server drains before the database
beneath it closes. Startup within a stage is concurrent; a failed database ping fails startup,
and readiness never flips. The predecessor's composite shutdown hook, and its caveat about a
hung drain starving infrastructure teardown, are gone with it — ordering is the coordinator's,
declared per service at registration.

## The hermetic test contract

The suite runs with no live database, so it proves the cold start and the startup contract:
construction performs no I/O, a dead database fails `Run` to exit 1 without a ready record, and
the coordinator is single-use. The serve-probes-drain path is proven against the compose stack;
a database-backed suite arrives with the data layer. `internal/config/configtest` is the single
source of valid test configuration — a subsystem's required fields are set there once, and the
suites adapt.
