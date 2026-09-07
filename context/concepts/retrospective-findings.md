# Retrospective findings — the auth layer's session inputs

Recorded at the 2026-08-31 workspace retrospective, from a full evaluation of the service as
landed. What remains is the auth section; `goals.v1.auth` and `v1.auth.strategy` cite it, and
it decays when the strategy session consumes it. The other sections were consumed as their
sessions ran: the testing section on 2026-09-01 (the decisions live in standards-lab
`context/design/testing-hierarchy.md`), the domain-rewrite section on 2026-09-06 (the lock
registry, the shared matcher, the path parse, and the guarded-command read live in the `data`
and `sdk` packages; the verb rule is `design/domain-architecture.md`), and the listener section
when the listener became `v1.admin-listener` (the exploration is standards-lab
`context/concepts/admin-listener.md`). The 503 on a database outage, wired at the evaluation,
is proven by the integration tier (`integration/outage_test.go`). The evaluation's verdict for
balance: the composition root is the cleanest part of the repo, and the transfer
implementation is correct as written; the findings are headroom.

## For the auth layer (`goals.v1.auth`)

Seams that do not exist yet, better cut deliberately than mid-auth-session:

- **No home for infrastructure-backed middleware.** `routes(dom, adm, cfg)` does not receive
  `infra`, and the only middleware build point is `router.Use` — which wraps the entire
  dispatch including the probes (`/healthz`, `/readyz`), the one place tokens must not be
  required. The stated rule ("a middleware that has to reach a domain service is domain
  logic", `internal/app/middleware.go`) conflates domain middleware with infrastructure-backed
  middleware. Either `routes` receives `infra` or a third, group-scoped build point.
- **No request-identity carrier.** No context key package, no subject/claims accessor;
  service methods have no subject parameter — when authorization lands, every domain method
  signature changes at once, so the carrier design precedes the fourth domain.
- **The single-row path has no authorization seam.** The list path can AND an extra predicate
  into both count and page, but the one-row read accepts one equality and nothing else — a
  row-level grant filter on `GET /{id}` is not expressible. A requirement on `sqlate`'s
  projection to settle before auth, not mid-auth.
- **Subtree authorization forces the lineage decision** (`concepts/organization-lineage.md`):
  unit-scoped grants ask "is X under U?" per request against a per-statement recursive CTE,
  and `path` is computed, so filtering on it can never use an index.
- **DI is positional with no error path.** `organization.New(db)` and `internal/app`'s
  `newDomain(infra, lc)` (no error return): the roadmap adds a logger, tracer, authorization
  evaluator, and cross-domain interfaces — a per-domain deps struct and an error return on
  the domain constructor is a ten-line change now, a four-domain refactor later.
