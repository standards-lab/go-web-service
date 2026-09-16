# slab: conventions

The rules `tools/slab` follows, and a future scenario or subcommand should too. What slab is and
how to run it: [`tools/slab/README.md`](../../tools/slab/README.md).

## Module and dependencies

Its own Go module, rooted at `tools/slab`, with no dependency on `go-web-service`'s own root
module — `sqlate`, `go-web-sdk`, and `cobra` only. Cobra and anything else slab needs never enters
`cmd/server`'s dependency graph. `go-web-service` is versionless (no tags yet), so a dependency on
it would need an unstable pseudo-version pin, resolving to a stale commit in exactly the
environments — a fresh clone, CI — that lack the repo's gitignored `go.work` to paper over it;
local cross-module work goes through that `go.work` instead. The wire types slab sends and reads
are restated in `internal/api` rather than imported from `domain/organization`: a black-box HTTP
observer states the contract it expects, the way `integration/organization_test.go` already does,
so a server-side rename breaks the demo instead of silently following it.

## Package layout

- `internal/demo` — one file per scenario, holding only its scenario literal (`Name`/`Summary`/
  `Needs`/`Steps`) and the methods that execute its steps. No shared infrastructure lives here.
- `internal/api` — the service's HTTP contract as slab calls it: routes, wire types, and the
  calls more than one scenario makes identically (`Reset`, `List`, ...). A call moves here only
  when two scenarios make the identical call — a step whose value is showing its own literal
  request keeps that request in its scenario file, not here.
- `internal/statement` — the lookups over sqlate's compiled output `demo sqlate` narrates.
- `internal/httpx` — the protocol layer: the client, `Response`, and anything about HTTP that
  isn't specific to this service (`IfMatch`, `RawQuery`, `Problem`, `TraceID`, the `Live` probes).
- `internal/env` — the run configuration (`Env`) every layer reads from context. No dependency on
  any other slab package, so `httpx` and `repo` can read it directly without an import cycle.
- `internal/repo` — the repository root resolver.
- `internal/scenario` — `Scenario`, `Step`, `Need`, `Reporter`, and the registry `internal/cli`
  builds subcommands from.
- `internal/cli` — the cobra command tree; builds one subcommand per registered scenario
  automatically off the registry, so a new scenario needs no change here.

## Naming

- A root subcommand is bare, never capability-prefixed: `demo` is already the capability word, so
  a scenario under it needs only its own name (`demo domain`, not `demo domain-crud`).
- A step's Go method name is its `Intent` in lowerCamel, articles and prepositions dropped, a
  nominalized title becoming the verb it nominalizes (`"Results"` → `results`, `"Catalog
  registration"` → `registerCatalog`).

## The scenario mechanism

A scenario is an ordered sequence of steps; a step is an intent sentence (`Step.Intent`, the
narrated heading), an action (`Step.Action`, what runs), and an observation (what the action
prints through the `Reporter` it is given). Two observation channels exist today:

- **Request/response** — `Reporter.Request` then `Reporter.Response`.
- **Observability side-channel** — the same, plus `Reporter.Trace`: a terse pointer (Grafana's
  Explore URL, the service name, the trace id read off `X-Request-Id`), not a deep link — a deep
  link's percent-encoded pane state is longer than the three lines and no easier to follow.

A `Need` states one precondition a scenario requires and the `mise` task that satisfies it,
checked before the first step. A `Check` that only reads `env.FromContext(ctx)` can be a bare
function reference (`httpx.Live`, `httpx.GrafanaLive`) rather than a closure.

A repeating, non-returning step fits this same shape without a mechanism change, if a future
scenario ever needs one — nothing built so far has.
