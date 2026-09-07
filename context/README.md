# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, go-database, and sqlate, and demonstrates each capability in
place. It
grows in documented layers on the go-web-sdk-template baseline it was generated from, consuming
the SDKs at pinned releases. This is the service level — where patterns are proven in a running
composition before they promote outward into the SDKs, the template, and the standard.

The service runs on one declared stack — Postgres for SQL — and uses each capability at the
resolution its purpose requires: the library's standard tier by default, and the provider's
native features where they earn it, contained and listed (`design/stack.md`). What version 1.0
is and the path to it live in the workspace roadmap at the coordinator (standards-lab
`context/roadmap.toml`, goal `v1`); the concepts here carry each layer's design direction. The
change and release discipline is described in `design/documented-layers.md`.

## Capability map

Broad and unordered; each layer is detailed only when a session is about to build it. The
baseline is running: `cmd/server`, the config files, and the README are authoritative for the
composition root, configuration bootstrap, logging, lifecycle, HTTP server, probes, and the
database with its admin service (startup migration, the configured seed set, the named states,
and the admin mount), all wired over the SDKs and sqlate at pinned releases; the root
`integration` package is the integration tier that asserts the running service through its
API. The layers are the `v1` goals in the workspace roadmap, which sequences them; each is
named here with the concept that carries its direction:

- **Data composition and CQRS** — the organization domain runs on authored SQL; the remaining
  domains and the CQRS contract are `concepts/data-layer.md`.
- **Auth** — authentication over OAuth 2.0 and OIDC, and an authorization model the strategy
  session chooses by analysis rather than assumes (`concepts/identity-linking.md`,
  `concepts/retrospective-findings.md`).
- **The management listener** — the admin mount on its own listener, a composition-root
  reshape here and in the template; the exploration is the coordinator's
  `concepts/admin-listener.md`.
- **Observability** — logging, metrics, and tracing through OpenTelemetry across the stack.
- **Object storage** — the go-storage capability demonstrated in a documented layer.
- **Messaging and reactor services** — NATS through go-messaging, and the reactor layer it
  makes real.
- **AI** — the go-ai capability demonstrated in a documented layer.
- **Embedded client** — a web client embedded in and served by the service binary.
- **Deployment** — deployment infrastructure in its own repository, targeting this service.
