# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, go-database, and sqlate, and demonstrates each capability in
place. It grows in documented layers on the go-web-sdk-template baseline it was generated from, consuming
the SDKs at pinned releases. This is the service level: patterns are proven here, in a running
composition, before they promote outward into the SDKs, the template, and the standard.

The service runs on one declared stack (Postgres for SQL), and uses each capability at the
resolution its purpose requires: the library's standard tier by default, and the provider's
native features where they earn it, contained and listed (the README's Stack section). What
version 1.0 is and the path to it live in the workspace roadmap at the coordinator (goal `v1`);
the notes here carry each layer's design direction. `CLAUDE.md` states the change and release
discipline. `domain-architecture.md` holds the rules every domain layer is built by, and
`integration-tier.md` what the integration tier grows next.

## Capability map

Broad and unordered; each layer is detailed only when a session is about to build it. The
baseline is running: `cmd/server`, the config files, and the README are authoritative for the
composition root, configuration bootstrap, logging, lifecycle, HTTP server, probes, the database
with its admin service (startup migration, the configured seed set, the named states, and the
admin mount), and observability (a telemetry layer bracketing the lifecycle stages, tracing and
metrics over OTLP, and structured logs correlated to them by trace id), all wired over the SDKs,
go-observability, and sqlate at pinned releases; the root `integration` package is the
integration tier that asserts the running service through its API. The remaining layers are the
`v1` goals in the workspace roadmap, which sequences them; each is named here with the concept
that carries its direction:

- **Data composition and CQRS** — the organization domain runs on authored SQL; the remaining
  domains and the CQRS contract are `data-layer.md`.
- **Auth** — authentication over OAuth 2.0 and OIDC, and an authorization model evaluated as SQL
  against the service's own grant data; the strategy is the coordinator's auth strategy.
- **The management listener** — the admin mount on its own listener, a composition-root
  reshape here and in the template; the exploration is the coordinator's admin-listener note.
- **Object storage** — the go-storage capability demonstrated in a documented layer.
- **Messaging and reactor services** — NATS through go-messaging, and the reactor layer it
  makes real.
- **AI** — the go-ai capability demonstrated in a documented layer.
- **Embedded client** — a web client embedded in and served by the service binary.
- **Deployment** — deployment infrastructure in its own repository, targeting this service.
