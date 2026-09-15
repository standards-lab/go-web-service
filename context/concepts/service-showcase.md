# Showing the workspace off

Colleagues and leadership need to see the workspace's capabilities directly — real running code
and real signals, not a narrated tour or slides — and anyone exploring on their own needs the same
path in. `slab` is the answer: a CLI in its own module, `go-web-service/tools/slab`
(`github.com/standards-lab/go-web-service/tools/slab`), built on `cobra`. One root command, with
subcommands layered by capability (`slab list`, `slab run <scenario>`, namespaced roughly
`<capability>:<scenario>` — `sqlate:compile`, `organization:crud`, `observability:traffic`,
`admin:migrate`). Anyone with `go-web-service` cloned already has it; no other repo needs cloning.

## Why a module of its own, inside go-web-service

Every capability worth demoing already flows through `go-web-service` by design — it is the one
place the workspace's layers converge. A standalone repository would add release and CI surface
the workspace doesn't otherwise need. Per-repo `mise` tasks would keep every repo unaware of the
others, but `mise` isn't shaped for a process that doesn't return (the traffic generator), and
gives no single discovery point. Locating `slab` inside `go-web-service` as its own module — the
same shape `sqlate/sqlint` and `sqlate/postgres` already use — gets the discovery point and the
orchestration power without either cost: `cobra`, and anything else `slab` needs, never touches
`cmd/server`'s `go.mod`, so the production binary's dependency graph is untouched by anything the
demo tool pulls in.

`cobra` itself is adopted, not hand-rolled, against `dependency-sourcing.md`: it is used by the
projects that define the Go CLI ecosystem (`kubectl`, Helm, Hugo, Docker, `gh`), carries a
near-zero transitive footprint (`spf13/pflag`), and has held a stable, mostly-fixes v1.x line for
years — nested-flag scoping and multi-shell completion are exactly the accumulated-corner-case
work that rule reserves for sourcing over hand-rolling.

## How a scenario reaches its target

- Against `sqlate`: `slab` imports the library directly (plus `sqlate/postgres` for the
  dialect-rendered step) and runs the compile pipeline in-process — no network hop, no database.
- Against `go-web-service` itself: `slab` is an HTTP client against the already-running service
  and its `/admin/database` mount, the same shape the `integration/` package's `webtest`-based
  harness already uses. `slab` assumes the compose stack (`mise run otel-up`, `mise run serve`,
  etc.) is already up; a scenario that can't reach the service names the `mise` task to run first
  rather than driving the stack itself.

`slab` needs no dependency on `go-web-service`'s own root module at all — only `sqlate`,
`go-web-sdk`, and `cobra`. Local cross-module work goes through the repo's existing gitignored
`go.work`, extended to cover `tools/slab`.

## One mechanism, three signal shapes

A scenario is an ordered sequence of steps; a step is an intent sentence, an action, and an
observation. What varies is the observation channel and whether the action returns immediately:

- **Request/response** (organization CRUD, admin migrate/verify/seed/state) — the observation is
  the HTTP response.
- **Observability side-channel** — the action is a request as before, but the observation also
  names the resulting trace (a Tempo/Grafana deep link — `datasources.yaml` already cross-links
  Tempo and Loki by fixed uid with `tracesToLogsV2`) and its correlated log line.
- **Long-running background action** — the traffic generator's action doesn't return; it loops
  issuing varied requests until interrupted, so Tempo, Loki, and Mimir accumulate real signal for
  a live Grafana walkthrough. Same step/observation shape, a repeating action rather than a
  one-shot one.

`v1.messaging`'s reactor signal will be a fourth observation channel — state read back after an
event rather than a direct response — not a new mechanism.

## Staged across two start sessions

Session 1 proves the mechanism on one scenario per novel shape, not the full domain surface:
`sqlate:compile`, one `go-web-service` request-and-trace scenario, and the traffic generator. It
also settles, as part of its own build: whether the compile demo's most interesting intermediate
artifact — the post-splice, pre-placeholder-rewrite SQL body, which sits behind the unexported
`Catalog.expand` (`sqlate/query/patterns.go:346`) — is useful enough beyond this one demo to
justify `sqlate` exporting a small inspection hook (in which case `sqlate` joins this task's
`repos`), or whether `slab` narrates from the already-public surface (`Statement.Text()`,
`Catalog.Patterns()`, `Statement.Params()`) instead.

Session 2 extends the proven mechanism to the remaining surface: full organization-domain CRUD
and the admin `/admin/database` migrate/verify/seed/state walk.

## Assumptions this rests on

- That `go-web-service` keeps being the workspace's point of convergence for every capability
  worth demoing; a future capability with no relationship to it at all would need this note
  revisited.
- That the compose stack's Grafana provisioning keeps its fixed datasource uids and
  `tracesToLogsV2` linkage as the observability stack evolves.
