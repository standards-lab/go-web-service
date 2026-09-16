# Showing the workspace off

Colleagues and leadership need to see the workspace's capabilities directly — real running code
and real signals, not a narrated tour or slides — and anyone exploring on their own needs the same
path in. `slab` is the answer: a CLI in its own module, `go-web-service/tools/slab`
(`github.com/standards-lab/go-web-service/tools/slab`), built on `cobra`. One root command, with
one `demo` subcommand per scenario (`slab list`, `slab demo <scenario>` — `sqlate`, `domain`, and
`observability` once built), each name bare rather than capability-prefixed: `demo` is already the
capability word, so a scenario needs only its own name under it. Anyone with `go-web-service`
cloned already has it; no other repo needs cloning.

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
  harness already uses — its own client, not an import of `webtest`, since that package's
  `testing.TB` binding has no place in a command's binary. `slab` assumes the compose stack
  (`mise run otel-up`, `mise run serve`, etc.) is already up; a scenario that can't reach the
  service names the `mise` task to run first rather than driving the stack itself.

`slab` needs no dependency on `go-web-service`'s own root module at all — only `sqlate`,
`go-web-sdk`, and `cobra`. Local cross-module work goes through the repo's existing gitignored
`go.work`, extended to cover `tools/slab`.

## One mechanism, three signal shapes

A scenario is an ordered sequence of steps; a step is an intent sentence, an action, and an
observation. What varies is the observation channel and whether the action returns immediately:

- **Request/response** (organization CRUD, admin migrate/verify/seed/state) — the observation is
  the HTTP response.
- **Observability side-channel** — the action is a request as before, but the observation also
  names the resulting trace. Not a deep link: embedding Grafana's pane state in a URL query value
  is unavoidably a long percent-encoded string — that is what putting JSON in a query string
  costs, not a gap in the tooling — so the observation is a terse pointer instead, one a viewer
  navigates by hand: Grafana's Explore URL, the service name, and the trace id.
- **Long-running background action** — the traffic generator's action doesn't return; it loops
  issuing varied requests until interrupted, so Tempo, Loki, and Mimir accumulate real signal for
  a live Grafana walkthrough. Same step/observation shape, a repeating action rather than a
  one-shot one.

`v1.messaging`'s reactor signal will be a fourth observation channel — state read back after an
event rather than a direct response — not a new mechanism.

## Staged across two start sessions

Session 1 settled the mechanism and, along the way, covered more of the surface than first
planned:

- `sqlate` narrates four steps — write a pattern, write a statement that includes it, register
  both against a catalog (one line each), show the compiled output — over one real pairing
  (`data/patterns/identity.sql` and `domain/organization/statements/create.sql`), not a tour of
  the library's whole surface. The export question is settled: no export. `slab` narrates from
  `sqlate`'s already-public surface (`Statement.Text()`, `Statement.Params()`,
  `Catalog.Patterns()`) directly, without reconstructing an intermediate compile artifact by hand
  — the demo never needed to raise the question the original plan expected it to.
- `domain` covers the organization domain's full CRUD in one scenario — initialization (reset to
  `data/seeds/default.json`), a raw list, a filtered/paged list, find, find by path, create (the
  observability side-channel), edit, transfer, a second raw list showing what the writes did, and
  delete — rather than proving the request/response shape on one call and deferring the rest.
  Resetting to a known seed at the start of every run is what makes this safe: `edit` and
  `transfer` mutate real seeded rows (`finance`, `logistics`), not a throwaway fixture, and the
  next run's reset is what undoes it, not this run's own cleanup.

What remains of session 1's task (`slab.tasks.mechanism`): the traffic generator, the third novel
shape, not yet built.

Session 2 (`slab.tasks.surface`) narrows to what `domain` didn't already cover: the admin
`/admin/database` migrate/verify/seed/state walk.

## Assumptions this rests on

- That `go-web-service` keeps being the workspace's point of convergence for every capability
  worth demoing; a future capability with no relationship to it at all would need this note
  revisited.
- That `data/seeds/default.json` stays a stable, known fixture: `domain` resets to it by name and
  mutates two of its rows (`finance`, `logistics`) by code. A seed change that renames or removes
  either, or a schema change that breaks the `/admin/database/state` reset, needs this scenario
  revisited alongside it.
