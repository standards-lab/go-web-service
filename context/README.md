# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, and go-database, and demonstrates each capability in place. It
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
database with its admin service (startup migration and seeding, and the admin mount), all wired
over the SDKs and sqlate at pinned releases. The layers are the `v1` goals in the workspace
roadmap:

- **Data composition and CQRS** — the layer in flight: a composed data model over plain SQL and
  a CQRS-oriented interface (`concepts/data-layer.md`).
- **Auth and ABAC** — authentication providers behind one interface, and verb-keyed
  attribute-based access control over the composed model (`concepts/identity-linking.md`).
- **Observability** — logging, metrics, and tracing through OpenTelemetry across the stack.
- **Object storage** — the go-storage capability demonstrated in a documented layer.
- **Messaging and reactor services** — NATS through go-messaging, and the reactor layer it
  makes real.
- **AI** — the go-ai capability demonstrated in a documented layer.
- **Embedded client** — a web client embedded in and served by the service binary.
- **Deployment** — deployment infrastructure in its own repository, targeting this service.
