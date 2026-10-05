# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, go-database, sqlate, go-storage, blobfs, and go-observability,
and demonstrates each capability in place. It grows in documented layers on the go-web-sdk-template baseline it was generated from,
consuming the SDKs at pinned releases. This is the service level: patterns are proven here, in a
running composition, before they promote outward into the SDKs, the template, and the standard.

The service runs on one declared stack (Postgres for SQL, Azure Blob Storage for objects) and
uses each capability at the tier its purpose requires: the library's standard tier by default,
and the provider's native features where they earn it, contained and listed (the README's Stack
section). The workspace roadmap in the coordinator repository, standards-lab, defines version
1.0 and the path to it (goal `v1`). The notes here carry each layer's design direction:

- `data-layer.md` holds the data composition and CQRS layer's strategy and planned domain model.
- `domain-architecture.md` holds the rules every domain layer is built by.
- `integration-tier.md` holds what the integration tier adds next.

`STANDARDS.md` states the change discipline and the domain layer's judgement calls.

## Capability map

Broad and unordered; each layer is detailed only when a session is about to build it. The
baseline is running: `cmd/server`, the config files, and the README are authoritative for the
composition root, configuration bootstrap, logging, lifecycle, HTTP server, probes, the database
with its admin service (startup migration, the configured seed set, the named states, and the
admin mount), observability (a telemetry layer bracketing the lifecycle stages, tracing and
metrics over OTLP, and structured logs correlated to them by trace id), and object storage (an
organization's logo and its document hierarchy on blobfs over go-storage, proxied through the
service, with the sweep that finishes a recursive delete), all wired over the SDKs,
go-observability, go-storage, blobfs, and sqlate at pinned releases; the root `integration`
package is the integration tier that asserts the running service through its API. The remaining layers are the
`v1` goals in the workspace roadmap, which sequences them; each is named here with the note
that carries its direction:

- **Data composition and CQRS** — the organization and document domains run on authored SQL;
  `data-layer.md` carries the remaining domains and the CQRS contract.
- **Auth** — authentication over OAuth 2.0 and OIDC, and an authorization model evaluated as SQL
  against the service's own grant data; the coordinator's auth-strategy note carries the
  strategy.
- **The management listener** — the admin mount on its own listener, a composition-root
  reshape here and in the template; the coordinator's admin-listener note explores it.
- **Messaging and reactor services** — NATS through go-messaging, and the reactor layer it
  makes real; the reactor is staged in `sdk` and already runs the sweep.
- **AI** — the go-ai capability demonstrated in a documented layer.
- **Embedded client** — a web client embedded in and served by the service binary.
- **Deployment** — deployment infrastructure in its own repository, targeting this service.
