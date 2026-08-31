# Retrospective findings — the service's session inputs

Recorded at the 2026-08-31 workspace retrospective, from a full evaluation of the service as
landed (`v1.data.writes.organization`). Grouped by the session that consumes each finding;
the roadmap tasks cite this note (`v1.data.sql.organization`, `v1.data.sql.startup`,
`goals.v1.auth`, `v1.testing`). It decays as those sessions consume it. The evaluation's
verdict for balance: the composition root is the cleanest part of the repo — construction
does no I/O, registration happens at construction, ordering is delegated, probes query the
live coordinator — and the transfer implementation is correct as written; the findings are
headroom.

## For the domain rewrite (`v1.data.sql.organization`)

Extract before domains multiply — four copies of each is the alternative:

- **The advisory-lock key registry leaves the domain package.** `const treeLock int64 = 1`
  (`domain/organization/database.go:23`) is documented as "the service's advisory-lock key
  registry" but lives inside one domain. Postgres advisory keys are database-global; people
  and inventory both want locks, and nothing today prevents a collision. A service-level
  registry, before domain two.
- **The shared status matcher.** ~90% of `handler.go`'s `status()` switch is library
  vocabulary identical for every domain (`QueryError`, `UnknownFieldError`,
  `UnknownOperatorError`, `ErrUniqueViolation`, `ErrForeignKeyViolation`,
  `ErrVersionMismatch`, `sql.ErrNoRows`, `PreconditionError`); only `ErrCycle` is
  organization's. `ErrorWriter` already takes a matcher list, first match wins — compose
  `web.NewErrorWriter(orgStatus, sdk.CommonStatus)` instead of copying the switch (four
  places to forget 412).
- **`decode[T]`, `pathID`, and the `ErrValidation`-wrapping UUID parse** are domain-independent
  and promote (decode and the If-Match parse to go-web-sdk via `v1.web.adapter`; the UUID
  path helper wherever the adapter session places it).
- **Do not copy forward**: the `UnknownOperatorError` matcher branch is unreachable —
  `directives()` only ever emits `OpEq`, so filters are exact-match-only and the branch
  entrenches a vocabulary fiction; PATCH-with-PUT-semantics on `Edit` (omit `name` → 400 —
  full replacement on a PATCH verb); and `TransferOrganization.ParentID *string` treating
  omitted and explicit null identically, so a transfer body of `{}` silently re-roots the
  organization — for a destructive structural move, require the key.

Error semantics to settle in the rewrite (they interlock with go-database's
`concepts/v0.4-findings.md` items 4–5): a malformed filter value is a typed 400, not a 22P02
cast error surfacing as 500; a database outage (`ErrNotReady`/`ErrConnectionFailed`) answers
503, not 500.

## For startup and the management listener (`v1.data.sql.startup`)

- go-web-sdk's env composition hardcodes the `"server"` segment, so a second `web.Config`
  block cannot exist under one prefix — the SDK-side fix is on
  `go-web-sdk/context/concepts/error-handling.md` §2.6.
- The composition root is singular by shape, not by parameter: `App` holds one `server`
  field, `routes()` feeds one router, `middleware()` is one stack, `RegisterHealth` is called
  once. The second listener is a real reshape of `internal/app`, and the template takes the
  same reshape in its refresh (`v1.data.sql.hardening`).
- Developer-loop drift found around the compose stack: `compose/postgres.yml` honors
  `POSTGRES_PORT`/`POSTGRES_USER`/`POSTGRES_DB` while `config.json` declares none of them —
  set one and the tooling silently migrates/seeds/serves against the wrong database until
  `APP_DATABASE_*` is also set. `secrets.json` is a manual `echo` with no
  `secrets.example.json`, failing late at `db.Start`. `APP_ENV=local` exists only inside
  mise. (`backlog.workspace-sweep` carries these; listed here because the startup rework
  touches the same files.)

## For the auth layer (`goals.v1.auth`)

Seams that do not exist yet, better cut deliberately than mid-auth-session:

- **No home for infrastructure-backed middleware.** `routes(dom, cfg)` does not receive
  `infra`, and the only middleware build point is `router.Use` — which wraps the entire
  dispatch including the probes (`/healthz`, `/readyz`), the one place tokens must not be
  required. The stated rule ("a middleware that has to reach a domain service is domain
  logic", `internal/app/middleware.go`) conflates domain middleware with infrastructure-backed
  middleware. Either `routes(infra, dom, cfg)` or a third, group-scoped build point.
- **No request-identity carrier.** No context key package, no subject/claims accessor;
  service methods have no subject parameter — when ABAC lands, every domain method signature
  changes at once, so the carrier design precedes the fourth domain.
- **The single-row path has no authorization seam.** The list path can AND an extra predicate
  into both count and page, but the one-row read accepts one equality and nothing else — a
  row-level grant filter on `GET /{id}` is not expressible. A `query`-mechanism requirement to
  discover now (`v1.data.sql.plan`), not mid-auth.
- **Subtree authorization forces the lineage decision** (`concepts/organization-lineage.md`):
  unit-scoped grants ask "is X under U?" per request against a per-statement recursive CTE,
  and `path` is computed, so filtering on it can never use an index.
- **DI is positional with no error path.** `organization.New(db)` and
  `internal/domain.New(infra)` (no error return): the roadmap adds a logger, tracer,
  authorization evaluator, and cross-domain interfaces — a per-domain deps struct and
  `New(infra) (*Domain, error)` is a ten-line change now, a four-domain refactor later.

## For the testing session (`v1.testing`)

The evaluation's coverage facts, as inputs: every function in
`domain/organization/database.go` and five of six service methods sit at 0.0% — the transfer
cycle rejection, concurrent transfers under the advisory lock, the guard's 404-vs-412 split,
the RETURNING scan, the recursive projection, `NULLS NOT DISTINCT` root-code uniqueness, path
recomposition after transfer, migration DDL, and seed idempotency are proven only by the
manual compose-stack ritual, which is documented nowhere (the README shows curl for the
probes and nothing for `/api/organizations`). CI runs no database. `handler_test.go` is a
good rejection-path suite (`organization.New(nil)`, honestly labeled) but is the whole story.
The session decides the tiers, their cadence against the per-PR unit rate, and the
against-database suite's shape.

## Smaller items with no owning session

Carried on `backlog.workspace-sweep` unless a session reaches them first: the README documents
probes but not the API; no handler-level deadline exists and the SDK-default write timeout is
15 minutes (closed by `v1.web.middleware`'s timeout + body limit); `internal/reactors.New`
ignores all three parameters (honest placeholder — the first real reactor will reshape it,
accepted).
