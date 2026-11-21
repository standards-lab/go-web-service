# go-web-service context

The reference web service of the Standards Lab reference architecture: a single cohesive service
that composes go-core, go-web-sdk, go-database, sqlate, go-storage, blobfs, go-observability, and
go-messaging, and demonstrates each capability in place. It grows in documented layers on the
go-web-sdk-template baseline it was generated from, consuming the SDKs at pinned releases. This is the service level: patterns are proven here, in a
running composition, before they promote outward into the SDKs, the template, and the standard.

The service runs on one declared stack (Postgres for SQL, Azure Blob Storage for objects) and
uses each capability at the tier its purpose requires: the library's standard tier by default,
and the provider's native features where they earn it, contained and listed
([`docs/stack.md`](../docs/stack.md)). The workspace roadmap in the coordinator repository
(`standards-lab/roadmap/index.toml`) defines version 1.0 and the path to it (goal `v1`).

The README, the pages under `docs/`, and each package's `doc.go` document this repository, and
`STANDARDS.md` states the change discipline and the domain layer's judgement calls. The notes here
carry the design direction of planned layers:

- `data-layer.md` holds the data composition and CQRS layer's strategy and planned domain model.
- `integration-tier.md` holds what the integration tier adds next.

## Capability map

- **Composition root and lifecycle** (built): [cmd/server/doc.go](../cmd/server/doc.go),
  [internal/app/doc.go](../internal/app/doc.go), and the README's
  [Getting started](../README.md#getting-started).
- **Configuration** (built): [internal/config/doc.go](../internal/config/doc.go) and
  [docs/configuration.md](../docs/configuration.md).
- **Stack and tiers** (built): [docs/stack.md](../docs/stack.md).
- **Database and its admin service** (built): [data/doc.go](../data/doc.go),
  [admin/database/doc.go](../admin/database/doc.go), and [docs/admin.md](../docs/admin.md).
- **Domain layers** (built): [domain/organization/doc.go](../domain/organization/doc.go),
  [domain/document/doc.go](../domain/document/doc.go), [docs/api.md](../docs/api.md), and
  [docs/domain-architecture.md](../docs/domain-architecture.md).
- **Object storage** (built): [admin/storage/doc.go](../admin/storage/doc.go) and
  [docs/stack.md](../docs/stack.md).
- **Observability** (built): [compose/README.md](../compose/README.md).
- **Messaging** (built): [docs/messaging.md](../docs/messaging.md).
- **Sweep** (built): [docs/sweep.md](../docs/sweep.md).
- **Integration tier** (built): [integration/doc.go](../integration/doc.go) and
  [docs/tests.md](../docs/tests.md).
- **slab** (built): [tools/slab/README.md](../tools/slab/README.md).
- **Tasks** (built): [docs/tasks.md](../docs/tasks.md).
- **Data composition and CQRS** (planned): [data-layer.md](data-layer.md).
- **Integration tier protocol helpers** (planned): [integration-tier.md](integration-tier.md).
- **Auth** (planned): the coordinator's `standards-lab/roadmap/v1.auth.toml` and
  `standards-lab/context/auth-strategy.md`.
- **The management listener** (planned): the coordinator's
  `standards-lab/roadmap/v1.admin-listener.toml`.
- **AI** (planned): the coordinator's `standards-lab/roadmap/v1.ai.toml`.
- **Embedded client** (planned): the coordinator's `standards-lab/roadmap/v1.client.toml`.
- **Deployment** (planned): the coordinator's `standards-lab/roadmap/v1.deployment.toml`.
