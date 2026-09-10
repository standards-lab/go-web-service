# The data layer

Design direction for the data composition and CQRS layer — the goal `v1.data` in the workspace
roadmap (standards-lab `context/roadmap.toml`), which holds the tasks and their sequencing.
This note carries what the roadmap does not: the layer's strategy, the settled direction for
the coming slices, and the domain model. Direction here is candidate until a task's session
settles it in plan mode; a section the code comes to express is deleted.

## Strategy

- The service demonstrates both tiers of the database capability on purpose: standard SQL
  in authored files throughout, and native Postgres features where they earn it, each in a
  file that declares it in its header and so enters the port list (`design/stack.md`).
  Standard SQL is preferred where it costs nothing; a native choice is a choice, named as such.
- Library-bound infrastructure is prototyped natively in the SDK repository that owns it:
  go-database for the persistence surface, go-web-sdk for the web surface. It's linked into
  this repository through the local gitignored `go.work` during development. Nothing is staged
  under a `pkg/` tree here.
- The layer spans the SDKs and this service. The reads and writes slices are coordinated
  sessions — a library slice and the service slice that proves it, planned together, each
  repository on its own branch with its own pull request. The domain slices are service-only.
- Documentation waits for the effort to close: integration documentation lands in the root
  `docs/` tier once the surfaces stop churning. The README carries only identity,
  getting-started, tasks, and configuration.

## Reads and writes — built

The organization domain runs on authored SQL (rewritten at `v1.data.sql.integration.service`,
2026-09-06); the code expresses the layout, and the rules the next layer is built by are
`design/domain-architecture.md` and `internal/app/doc.go`. Two settled choices stand: ids are
database-minted (`uuidv7()`) and RETURNING is the application's identity pattern, both on the
port list (`design/stack.md`). Base packages outside `internal/` are importable by other
modules, accepted for a reference service as a deliberate choice, the wiring kept
compiler-private under `internal/`. The cross-board evaluation (`v1.data.evaluation`)
remains.

## Domain direction (candidate until a task settles it)

The service models a human organization, structured with patterns drawn from game data systems:
catalog templates with owned instances, and category branches composed over a generic base.

- **organization** — built, recursive from the start: a self-referencing `parent_id`,
  sibling-scoped unique slug codes (the property that makes org paths unique),
  database-generated `uuidv7()` ids, and the path a read-time projection
  (`concepts/organization-lineage.md`).
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
- Soft delete as the standard's convention, recommended by the prototype's review and
  deferred at `v1.data.sql.integration.service` to the first domain that needs it: `delete`
  moves a record to the recycle bin, `restore` returns it, and `purge` removes it physically and
  is administrative. The pattern pair for the catalog adds a `deleted_at` column, a read model
  that excludes deleted rows with a recycle view beside it, partial unique indexes over live
  rows, and the delete pattern as an update.
- Cross-domain invariants are enforced two ways: an SQL check inside the transaction where the
  dependency runs downward (custody checks person status), and an interface declared by the
  consuming domain and injected at the composition root where the check would otherwise run
  upward (people declares a custody check so deactivation is blocked while custody is open;
  inventory implements it).
- Identity linking is deferred to the auth layer; see `concepts/identity-linking.md`.

CQRS is strict: queries return data; commands return identity and version only. The command
surfaces, routes, constraint names, and enum vocabularies are settled per task.

## Evaluation evidence (`v1.data.evaluation`)

On record for the cross-board evaluation: the `sdk` package stages two tenants, `PathID` and
`Command`, at `v1.data.sql.integration.service` for go-web-sdk. The `data` package holds the
shared status matcher and the directives lowering, which the template cannot scaffold while it
stays engine-free. A generic seed helper is a fit question for the evaluation: it promotes to
go-database when its shape is the library's own, not when a second service repeats the
per-table loop.

## Prior R&D

`personnel-service-demo` (catalogued at the coordinator; local checkout
`~/s2va/personnel-service-demo`) is the input for the CQRS shape, the four-tier
business-logic placement, the error model, and the projection-driven data layer. It is input to
re-derive from, not a baseline to inherit. This reference stays in volatile context; the design
notes and the README justify every convention on its own merit.
