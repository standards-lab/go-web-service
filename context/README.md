# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, and go-database, and demonstrates each capability in place. It
grows in documented layers on the go-web-sdk-template baseline it was generated from, consuming
the SDKs at pinned releases. This is the service level — where patterns are proven in a running
composition before they promote outward into the SDKs, the template, and the standard.

The service runs on one declared stack — Postgres for SQL — and uses each capability at the
resolution its purpose requires: the library's standard tier by default, and the provider's
native features where they earn it, contained and listed (`design/stack.md`). What version 1.0
is, and what every layer must show on the way there, is `design/end-state.md`; the ordered path
is `concepts/roadmap.md`. The change and release discipline is described in
`design/documented-layers.md`.

The repository was created in relay step 6 of the coordinator's restructure
(standards-lab `concepts/restructure.md`): generated fresh from go-web-sdk-template
template/v0.3.0, with go-service's data capability composed onto the baseline. The roadmap and
the data ladder are carried from go-service as candidate direction; the re-plan session revises
them against the new repository structure before the next build.

## Capability map

Broad and unordered; each layer is detailed only when a session is about to build it. The
baseline is running — `cmd/server`, the config files, and the README are authoritative for the
composition root, configuration bootstrap, logging, lifecycle, HTTP server, probes, and the
database with its migrate and seed tooling, all wired over the SDKs at pinned releases.

- **Data composition and CQRS** — the first documented layer: a composed data model over plain
  SQL and a CQRS-oriented interface. The prior R&D is `personnel-service-demo` (strict CQRS,
  four-tier business-logic placement, the error model), catalogued at the coordinator.
- **Auth and ABAC** — authentication providers behind one interface, and verb-keyed
  attribute-based access control over the composed model.
- **Observability** — logging, metrics, and tracing across the stack.
