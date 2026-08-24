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

## Reads (`v1.data.reads`) — settled direction

Settled in the capability-tiers planning session (2026-08-17). go-database: the persistence
query vocabulary (page, sort, and exact-match filter directives), a projection with one key
field and name-to-expression fields, and a builder that emits standard SQL — placeholders
through the dialect, paging in the SQL:2008 `OFFSET … FETCH` form, count and page as two
statements, an unknown field a typed error. go-web-sdk: the parsing of `page`, `size`, and
`sort` (`sort=name,-code`) and the `items`/`page`/`size`/`total` envelope. This service:
`internal/domain/organization` with List, Find, and find-by-path over `GET /organizations`,
`GET /organizations/{id}`, and `GET /organizations/path/{path...}`; `path` on every read,
projected from the recursive lineage query in the projection's FROM
(`concepts/organization-lineage.md`); flat exact-match filters on projected fields; an unknown
sort or filter field answers 400; the import-boundary lint (`design/stack.md`) lands with this
first domain package. Remaining plan-mode detail when the task is reached: the shape of
`internal/domain/organization` — the projection with the lineage FROM, its query functions, and
its handlers — and the `.golangci.yml` allowlist.

## Writes (`v1.data.writes`) — candidate direction

go-database: the command result envelope, the optimistic-concurrency contract, and the error
model. This service: the organization command surface. Two native choices are decided here:
whether ids stay database-minted (`uuidv7()`, a Postgres builtin) or the application mints them
(`uuid.NewV7()`, portable), and `RETURNING` for the concurrency contract, which is not ISO SQL
and stays contained domain SQL.

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
behind it.

## Prior R&D

`personnel-service-demo` (catalogued at the coordinator; local checkout
`~/code/_s2va/personnel-service-demo`) is the input for the CQRS shape, the four-tier
business-logic placement, the error model, and the projection-driven data layer. It is input to
re-derive from, not a baseline to inherit. This reference stays in volatile context; the design
notes and the README justify every convention on its own merit.
