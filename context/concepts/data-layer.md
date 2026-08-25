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

Settled in the capability-tiers planning session (2026-08-17); promoted from a task to a goal
with three session-scoped tasks in the reads-replan session (2026-08-25), each repository on
its own branch and pull request:

- `v1.data.reads.query` — go-database's `query` package: page, sort, and exact-match filter
  directives; a projection with one key field and name-to-expression fields; a builder that
  emits standard SQL — placeholders through the dialect, paging in the SQL:2008
  `OFFSET … FETCH` form, count and page as two statements, an unknown field a typed error
  (go-database `context/concepts/query.md`).
- `v1.data.reads.web` — go-web-sdk's HTTP read contract: the parsing of `page`, `size`, and
  `sort` (`sort=name,-code`) and the `items`/`page`/`size`/`total` envelope (go-web-sdk
  `context/concepts/direction.md`).
- `v1.data.reads.organization` — this service's organization domain package with List, Find,
  and find-by-path over `GET /organizations`, `GET /organizations/{id}`, and
  `GET /organizations/path/{path...}`; `path` on every read, projected from the recursive
  lineage query in the projection's FROM (`concepts/organization-lineage.md`); flat exact-match
  filters on projected fields; an unknown sort or filter field answers 400; the import-boundary
  lint (`design/stack.md`) lands with this first domain package. Remaining plan-mode detail
  when the task is reached: the package's file shape — the projection with the lineage FROM,
  its query functions, and its handlers — the sdk staging package's first contents, and the
  `.golangci.yml` allowlist.

## Service layout (settled in the reads-replan session, 2026-08-25)

- `internal/` is the composition root only. `internal/{app,infrastructure,domain,reactors}`
  construct, register, and mount; they define no domain infrastructure. `internal/domain.New`
  constructs each domain package's services from infrastructure fields, and
  `internal/app/routes.go` mounts their modules — the two edit points the template already
  designates.
- Domain packages — entities, domain services, projections, and their HTTP handlers together —
  live at the module's base package layer, one package per domain. The grouping
  (`domain/organization` versus a root-level `organization`) is settled at the organization
  task's plan mode; the developer's instinct is `domain/organization`. Base packages outside
  `internal/` are importable by other modules — accepted for a reference service, and named
  here as a deliberate choice.
- Promotion candidates stage in a base `sdk` package: request/response conventions not yet
  library-worthy — first among them the domain-error-to-problem mapping for the reads 400/404
  paths — are fleshed out there and promote to go-web-sdk or go-database when their design
  settles. `v1.data.evaluation` is the scheduled checkpoint; every session in the goal names
  what should promote outward — to the `sdk` package, the libraries, or the template.
- Template implication on record: the template's `internal/domain` doc comment reads as if
  services are defined in place. Once the base-package layout is proven here, the template's
  doc.go and the landing-zone page should say defined in base packages, constructed in
  `internal/domain` — a promotion candidate for the evaluation task or an earlier coordinator
  sweep.

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
