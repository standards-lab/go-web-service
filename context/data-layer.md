# The data layer

This note holds the design direction for the data composition and CQRS layer: its strategy and
its planned domain model. The layer is goal `v1.data` in the workspace roadmap, which holds the
tasks and their order. Direction here is a candidate until a task's session settles it; a
section the code comes to express is deleted.

## Strategy

- The service demonstrates both tiers of the database capability on purpose: standard SQL
  in authored files throughout, and native Postgres features where they earn it, each in a
  file that declares it in its header and so enters the port list (the README's Stack section).
  Standard SQL is preferred where it costs nothing; a native choice is a choice, named as such.
- A library change is prototyped natively in the SDK repository that owns it: go-database for
  the persistence surface, go-web-sdk for the web surface. During development the local
  gitignored `go.work` links that repository into this one. A convention proven here before its
  library is settled stages in the flat root `sdk` package (`domain-architecture.md`); nothing
  is staged under a `pkg/` tree.
- The layer spans the SDKs and this service. The reads and writes slices are coordinated
  sessions — a library slice and the service slice that proves it, planned together, each
  repository on its own branch with its own pull request. The domain slices are service-only.
- Documentation waits for the effort to close: integration documentation lands in the root
  `docs/` tier once the surfaces stop churning. Until then the README carries identity, the
  stack, getting-started, the API and admin surfaces, tasks, tests, and configuration.

## Reads and writes

The organization and document domains run on authored SQL; `domain-architecture.md` holds the
rules the next layer is built by. Ids are database-minted (`uuidv7()`) and `RETURNING` is the
application's identity pattern, both on the port list. Base packages outside `internal/` are
importable by other modules, a deliberate choice for a reference service, with the wiring kept
compiler-private under `internal/`.

## Domain direction (candidate until a task settles it)

The service models a human organization, structured with patterns drawn from game data systems:
catalog templates with owned instances, and category branches composed over a generic base.

- **organization**: a self-referencing `parent_id`, sibling-scoped unique slug codes (the
  property that makes paths unique), and the path a read-time projection from a recursive CTE.
  The closure table that authorization needs is planned in the auth strategy at the
  coordinator repository, standards-lab.
- **people** (`v1.data.people`): `person` is the stable UUID anchor, with a record-status enum
  other domains react to, a unit FK, and activate, deactivate, and transfer-unit action
  commands.
- **inventory** (`v1.data.inventory`): `item_template` holds catalog reference data (name,
  category); `item_instance` holds the physical unit (serial, condition). Custody is a ledger:
  one open row per held instance (`returned_at` null), guarded by a partial-unique index;
  return closes the row, and transfer closes it and opens the next one in a single transaction.
  Issue, transfer, and return are action commands.
- **devices** (`v1.data.devices`): the one branch domain, demonstrating composition over
  inheritance. A device row extends `item_instance` one to one; the devices package imports
  inventory, and inventory never imports devices. Future categories follow the same pattern as
  their own packages.
- Soft delete as the standard's convention, planned for the first domain that needs it: `delete`
  moves a record to the recycle bin, `restore` returns it, and `purge` removes it physically and
  is administrative. The pattern pair for the catalog adds a `deleted_at` column, a read model
  that excludes deleted rows with a recycle view beside it, partial unique indexes over live
  rows, and the delete pattern as an update.
- Cross-domain invariants are enforced two ways, settled by the document layer
  (`STANDARDS.md`): an SQL check inside the transaction where
  the dependency runs downward (custody checks person status), and an interface declared by the
  consuming domain and injected at the composition root where the check would otherwise run
  upward (people declares a custody check so deactivation is blocked while custody is open;
  inventory implements it).
- Identity linking belongs to the auth layer: `person.id` is the stable anchor, and the
  coordinator's auth strategy links an issuer and subject to it.

CQRS is strict: queries return data; commands return identity and version only. The command
surfaces, routes, constraint names, and enum vocabularies are settled per task.

## Evaluation evidence (`v1.data.evaluation`)

The `v1.data.evaluation` task weighs this evidence. The `sdk` package stages `Command` for
go-web-sdk, and the reactor and the quiesce gate for go-core; `Command`'s path parse is already
promoted, as go-web-sdk v0.13.0's `PathUUID`. The `data` package holds the shared status matcher and
the directives lowering, which the template cannot scaffold while it stays engine-free. A generic
seed helper is a fit question for the evaluation: it promotes to go-database when its shape is the
library's own, not when a second service repeats the per-table loop. It also decides whether a
linter (`depguard`, denying the provider modules outside the composition root) enforces the provider
import boundary the README's Stack section states.

## When to build a general sweeper

The sweep worker (`data.Storage.SweepWorker`) is a standalone sweeper for blobfs's deletes and the
only reclamation the service runs. When another layer first needs sweeper-like reclamation
(outbox cleanup, expired sessions, soft-delete purge, or any cascade beyond pruning SQL rows),
that task builds a general sweeper and moves blobfs's sweep onto it as its first reclaimer. It
never builds a second standalone worker.

## Prior R&D

Prior R&D in the organization's private references annex is the input for the CQRS shape, the
four-tier business-logic placement, the error model, and the projection-driven data layer. It is
input to re-derive from, not a baseline to inherit. This reference stays in volatile
context; the design notes and the README justify every convention on its own merit.
