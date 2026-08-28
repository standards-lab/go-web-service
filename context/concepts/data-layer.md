# The data layer

Design direction for the data composition and CQRS layer — the goal `v1.data` in the workspace
roadmap (standards-lab `context/roadmap.toml`), which holds the tasks and their sequencing.
This note carries what the roadmap does not: the layer's strategy, the settled direction for
the coming slices, and the domain model. Direction here is candidate until a task's session
settles it in plan mode; a section the code comes to express is deleted.

## Strategy

- The service demonstrates both tiers of the database capability on purpose: the library's
  standard tier (ISO SQL through the wrapper and the query vocabulary) throughout, and native
  Postgres features where they earn it, each contained in a package that declares it and
  entered in the port list (`design/stack.md`). Standard SQL is preferred where it costs
  nothing; a native choice is a choice, named as such.
- Library-bound infrastructure is prototyped natively in the SDK repository that owns it —
  go-database for the persistence surface, go-web-sdk for the web surface — linked into this
  repository through the local gitignored `go.work` during development. Nothing is staged under
  a `pkg/` tree here.
- The layer spans the SDKs and this service. The reads and writes slices are coordinated
  sessions — a library slice and the service slice that proves it, planned together, each
  repository on its own branch with its own pull request. The domain slices are service-only.
- Documentation waits for the effort to close: integration documentation lands in the root
  `docs/` tier once the surfaces stop churning. The README carries only identity,
  getting-started, tasks, and configuration.

## Reads and the service layout — built

The reads slice landed across the three repositories — go-database v0.2.0's `query` package,
go-web-sdk v0.4.0's read contract, and this service's organization package
(`organization-reads` session, 2026-08-26) — and the code expresses it. The domain-layer
layout standard it settled and validated — role-aggregated files, the translation-file
boundary, the `/api` module, the `sdk` staging package, the operation-shape principle — is
`design/domain-architecture.md`. The staged library candidates and template implications have
all landed — go-database's and go-web-sdk's write releases and the template's
`compose-on-releases` session — with `v1.data.evaluation` the final audit; the base `sdk`
package emptied at `v1.data.writes.organization` and re-seeded with the If-Match precondition
parse. Base packages outside `internal/` are importable by
other modules — accepted for a reference service as a deliberate choice, the wiring kept
compiler-private under `internal/`.

## Writes (`v1.data.writes`) — built through the organization surface

The writes slice landed across the stack — go-database v0.3.0's command contract, go-web-sdk
v0.5.0's ErrorWriter, and this service's organization commands (`organization-commands`
session, 2026-08-28) — and the code expresses it. Both native choices settled: ids stay
database-minted (`uuidv7()`), and RETURNING is consumed through the dialect's renderer,
contained in the library layers; the port list carries both. The command-side layout rules the
next layer builds by are `design/domain-architecture.md`. The holistic operation pass
(`v1.data.writes.operations`) remains, with this session's ergonomics findings on its record
in the workspace roadmap.

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
- Cross-domain invariants are enforced two ways: an SQL check inside the transaction where the
  dependency runs downward (custody checks person status), and an interface declared by the
  consuming domain and injected at the composition root where the check would otherwise run
  upward (people declares a custody check so deactivation is blocked while custody is open;
  inventory implements it).
- Identity linking is deferred to the auth layer; see `concepts/identity-linking.md`.

CQRS is strict: queries return data; commands return identity and version only. The command
surfaces, routes, constraint names, and enum vocabularies are settled per task.

## Evaluation evidence (`v1.data.evaluation`)

On record for the cross-board evaluation: `cmd/db` is heavy boilerplate to rewrite per service
— the dispatch, verb, and construction layers want a cheaper per-service shape — and the
migrate-wrapper direction culled in the migrations session gets re-asked there with real usage
behind it. The strict command-body decoder (`decode[T]` in the organization handler:
MaxBytesReader, DisallowUnknownFields) is a go-web-sdk promotion candidate to rule on
alongside the staged If-Match parse.

## Prior R&D

`personnel-service-demo` (catalogued at the coordinator; local checkout
`~/code/_s2va/personnel-service-demo`) is the input for the CQRS shape, the four-tier
business-logic placement, the error model, and the projection-driven data layer. It is input to
re-derive from, not a baseline to inherit. This reference stays in volatile context; the design
notes and the README justify every convention on its own merit.
