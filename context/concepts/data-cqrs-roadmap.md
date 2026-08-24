# Data composition and CQRS: session roadmap

A planning session (2026-08-12) scoped the first documented layer and found it too large for one
session. This note records the settled strategy and the ladder of sessions that builds the layer.
Each rung is one marathon session, and each rung's session settles its own API in plan mode; the
direction recorded here is candidate direction until a rung settles it.

**Relay note (2026-08-24).** Rungs 1 and 2 were built under the predecessor repositories:
go-libraries carried the library surface and go-service the service. The relay split that surface
— the database capability now lives in go-database, web routing in go-web-sdk — and this
repository was generated fresh from go-web-sdk-template with the built rungs' features composed
in (`design/composition-root.md`). The coordination and dev-tag mechanics below describe the
predecessor arrangement and are stale; the re-plan session revises this note, the roadmap, and
the release choreography against the new repository structure before rung 3 builds.

## Strategy

- The service runs on one declared stack — Postgres — and demonstrates both tiers of the database
  capability on purpose: the library's standard tier (ISO SQL through the wrapper and the query
  vocabulary) throughout, and native Postgres features where they earn it, each contained in a
  package that declares it and entered in the port list (`design/stack.md`). Standard SQL is
  preferred where it costs nothing; a native choice is a choice, named as such.
- Library-bound infrastructure is prototyped natively in the SDK repository that owns it —
  go-database for the persistence surface, go-web-sdk for the web surface — linked into this
  repository through a local `go.work` during development (gitignored here). Nothing is staged
  under a `pkg/` tree in this repository.
- The layer spans the SDKs and this service. Rungs 3–4 are coordinated sessions: a library slice
  and the service slice that proves it, planned together, each repository on its own branch with
  its own pull request. Rungs 5–7 are service-only.
- The predecessor dev-tag choreography (go-libraries `v0.5.0-dev.N`, postgres `v0.2.0-dev.N`,
  closing releases cut together after rung 8) is stale with the module split; the re-plan
  settles the equivalent for go-database and go-web-sdk, and this service's own first release.
- Documentation waits for the effort to close: integration documentation lands in its own root
  `docs/` tier linked from the README, written once the surfaces stop churning. The README
  carries only identity, getting-started, tasks, and configuration.

## The ladder

1. **Connectivity** (coordinated) — built, under the predecessors. The base `database` package
   (wrapper, dialect seam, config block, lifecycle and readiness) and `postgres` open and ping;
   the service's database configuration block, composition-root wiring, compose fragment, and a
   live database readiness check. The proof ran: the service boots against the compose postgres,
   `/readyz` turns 503 during an outage and heals when the database returns, and a failed
   startup ping fails fast.
2. **Migrations** (coordinated) — built, under the predecessors. The library slice became
   `database/seed`; the planned `database/migrate` wrapper was culled in plan mode — migrations
   standardize here, with golang-migrate consumed directly in `cmd/db` and the conventions
   encapsulated in named functions (`newMigrator`, `noChangeOK`). The service side: `cmd/db`,
   `internal/process`, the `0001_organization` migration, and the seeded hierarchy. The proof
   ran: up, down, and re-up clean; the seed idempotent; the org path composed by lineage query.
3. **Reads** (coordinated) — planned; the decisions were settled in the capability-tiers
   planning session (2026-08-17). go-database: the persistence query vocabulary (page, sort, and
   exact-match filter directives), a projection with one key field and name-to-expression
   fields, and a builder that emits standard SQL — placeholders through the dialect, paging in
   the SQL:2008 `OFFSET … FETCH` form, count and page as two statements, an unknown field a
   typed error. go-web-sdk: the parsing of `page`, `size`, and `sort` (`sort=name,-code`) and
   the `items`/`page`/`size`/`total` envelope. This service: `internal/organization` with List,
   Find, and find-by-path over `GET /organizations`, `GET /organizations/{id}`, and
   `GET /organizations/path/{path...}`; `path` on every read, projected from the recursive
   lineage query in the projection's FROM (`concepts/organization-lineage.md`); flat exact-match
   filters on projected fields; an unknown sort or filter field answers 400; the import-boundary
   lint (`design/stack.md`) lands with the first domain package. Remaining plan-mode detail
   when the rung is reached: the shape of `internal/organization` — the projection with the
   lineage FROM, its query functions, and its handlers — and the `.golangci.yml` allowlist.
   Proof: paginated, filtered, sorted reads over HTTP, and a path lookup.
4. **Writes** (coordinated). go-database: the command result envelope, the
   optimistic-concurrency contract, and the error model. This service: the first domain's full
   command surface. Two native choices are decided here: whether ids stay database-minted
   (`uuidv7()`, a Postgres builtin) or the application mints them (`uuid.NewV7()`, portable),
   and `RETURNING` for the concurrency contract, which is not ISO SQL and stays contained
   domain SQL. Proof: the 400, 409, and 412 paths respond correctly.
5. **People domain** (service-only).
6. **Inventory domain** (service-only): the item template and instance split and the custody
   ledger.
7. **Devices branch domain** (service-only).
8. **Toolchain evaluation, then the close.** With all three domains built, evaluate
   go-web-service and go-web-sdk-template across the board before anything ships: is anything
   fundamentally broken or incorrect; what can be simplified and made easier to work with; what
   in the service belongs in the template; what in the service should become reusable library
   capability. Under the tiers the evaluation also audits the port list: where the service uses
   a native Postgres feature, whether each use is contained in a package that declares it, and
   whether the import-boundary lint belongs in the template as baseline tooling. Evidence
   already on record: `cmd/db` is heavy boilerplate to rewrite per service — the dispatch, verb,
   and construction layers want a cheaper per-service shape — and the migrate-wrapper direction
   culled in rung 2 gets re-asked here with real usage behind it. The refinements this
   evaluation earns are their own sessions. After them, the coordinated semantic releases, the
   dev-tag purge, and the layer's documentation in the `docs/` tier — including the page that
   names each capability's class and the port list — close the effort.

## Domain direction (candidate until a rung settles it)

The service models a human organization, structured with patterns drawn from game data systems:
catalog templates with owned instances, and category branches composed over a generic base.

- **organization** — built in rung 2 as `organization`, recursive from the start: a
  self-referencing `parent_id`, sibling-scoped unique slug codes (the property that makes org
  paths unique), database-generated `uuidv7()` ids, and the path a read-time projection (see
  `concepts/organization-lineage.md`).
- **people**: `person` is the stable UUID anchor, with a record-status enum other domains react
  to, a unit FK, and activate, deactivate, and transfer-unit action commands.
- **inventory**: `item_template` holds catalog reference data (name, category); `item_instance`
  holds the physical unit (serial, condition). Custody is a ledger: one open row per held
  instance (`returned_at` null), guarded by a partial-unique index; return closes the row, and
  transfer closes it and opens the next one in a single transaction. Issue, transfer, and
  return are action commands.
- **devices**: the one branch domain, demonstrating composition over inheritance. A device row
  extends `item_instance` one to one; the devices package imports inventory, and inventory
  never imports devices. Future categories follow the same pattern as their own packages.
- Cross-domain invariants are enforced two ways: an SQL check inside the transaction where the
  dependency runs downward (custody checks person status), and an interface declared by the
  consuming domain and injected at the composition root where the check would otherwise run
  upward (people declares a custody check so deactivation is blocked while custody is open;
  inventory implements it).
- Identity linking is deferred to the auth layer; see `concepts/identity-linking.md`.

CQRS is strict: queries return data; commands return identity and version only. The command
surfaces, routes, constraint names, and enum vocabularies are settled per rung.

## Prior R&D

`personnel-service-demo` (catalogued at the coordinator; local checkout
`~/code/_s2va/personnel-service-demo`) is the input for the CQRS shape, the four-tier
business-logic placement, the error model, and the projection-driven data layer. It is input to
re-derive from, not a baseline to inherit. This reference stays in volatile context; the design
notes and the README justify every convention on its own merit.
