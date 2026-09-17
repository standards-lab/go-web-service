# slab: conventions

The rules `tools/slab` follows, and a future scenario, subcommand, or domain should too. What
slab is and how to run it: [`tools/slab/README.md`](../../tools/slab/README.md), and
[`tools/slab/usage.md`](../../tools/slab/usage.md) for a walkthrough by hand.

## Module, dependencies, and the composition root

Its own Go module, rooted at `tools/slab`, with no dependency on `go-web-service`'s own root
module — `sqlate`, `go-web-sdk`, and `cobra` only. Cobra and anything else slab needs never enters
`cmd/server`'s dependency graph. `go-web-service` is versionless (no tags yet), so a dependency on
it would need an unstable pseudo-version pin, resolving to a stale commit in exactly the
environments — a fresh clone, CI — that lack the repo's gitignored `go.work` to paper over it;
local cross-module work goes through that `go.work` instead.

slab's own layout mirrors the Go Elemental composition-root/domain split `go-web-service` itself
follows: `internal/app` is the composition root, one file per layer, the only package that
constructs a dependency or names a mount; everything it mounts — each domain's commands, the
admin container, the demo tree — is a root-level package it calls and never builds itself.
`cmd/slab/main.go` is process entry alone: it traps the signal, derives the root context, and
calls `app.New(os.Stdout, os.Stderr).Run(ctx)`.

## Package layout

- `domain/<name>` (today: `domain/organization`) — the client-side counterpart of the service's
  own Domain Service over that domain, not a Domain Service itself: slab consumes the API, it
  doesn't present one. Four files: `doc.go`, `entities.go` (the domain's wire types, restated —
  a black-box HTTP observer states the contract it expects, the way
  `integration/organization_test.go` already does, so a server-side rename breaks slab instead of
  silently following it), `client.go` (routes and one method per endpoint, the package's sole
  importer of `httpx`), and `commands.go` (`Commands(newClient func() *Client) *cobra.Command`,
  the domain's cobra subtree).
- `admin/<name>` (today: `admin/database`) — the same four-file shape, for an admin service's
  exposure rather than a Domain Service. A sibling of `domain/`, never nested under it: the
  architecture distinguishes the two, and `go-web-service` itself keeps its own `domain/` and
  `admin/` as separate root-level trees. `entities.go` here restates request bodies only —
  responses print as raw JSON, so nothing decodes them, and restating a type nothing decodes is a
  contract no test checks.
- `demo` — one file per scenario, holding only its scenario literal (`Name`/`Summary`/`Needs`/
  `Steps`) and the methods that execute its steps, each exporting its constructor (`Compile`,
  `Organization`, `Problems`) rather than registering itself — there is no registry. `commands.go`
  holds `Scenarios()` (the explicit, ordered list) and `Commands()` (the `demo` subtree built from
  it, panicking on a duplicate name). `calls.go` holds what more than one scenario makes
  identically (`Reset`, `List`, ...); a step whose value is showing its own literal request keeps
  that request in its scenario file, not here.
- `output` — the shared result rendering every direct command (`org`, `admin`) needs and neither
  domain owns: `Response` prints a success body pretty, or the status line alone when it's empty,
  so a command like `org delete` is never silent; `Error` renders a failure, a `*ProblemError`'s
  members one per line or a plain error's message; `Expect` turns a mismatched status into that
  error, decoding the RFC 9457 document when there is one; `FixedCommand` builds a zero-input
  subcommand from any domain client's bound no-argument method — no `Client` type or generics
  needed, since a bound method value already has the shape `func(context.Context)
  (*httpx.Response, error)`.
- `input` — the shared request-input resolution every body-taking direct command needs: `Body`
  resolves `--body <json>` verbatim or a `fromFlags` value marshaled, the two mutually exclusive
  by cobra (one `MarkFlagsMutuallyExclusive("body", <field>)` call per field flag — a single call
  naming every flag together would wrongly make the field flags exclusive with each other too).
  `GuardedBody` does the same plus resolves an If-Match version: the `--version` flag wins when
  set, otherwise a top-level `"version"` key in `--body` supplies it and is stripped before
  sending, since the wire type it guards never carries the field and the service disallows
  unknown ones.
- `httpx` — the protocol layer: the client, `Response`, and anything about HTTP that isn't
  specific to this service (`IfMatch`, `RawQuery`, `Problem`, `TraceID`, the `Live` probes). An
  empty `[]byte` or `string` body is treated exactly like `nil` — no `Content-Type`, nothing
  sent — so a caller with an optional body passes what it has without converting it first.
- `env` — the run configuration (`Env`) every layer reads from context. No dependency on any
  other slab package, so `httpx` and `repo` can read it directly without an import cycle.
- `repo` — the repository root resolver.
- `statement` — the lookups over sqlate's compiled output `demo sqlate` narrates.
- `scenario` — `Scenario`, `Step`, `Need`, `Reporter`, `Command` (builds one cobra command from a
  `Scenario` a caller hands it explicitly), and `WriteListing`. No registry: every scenario is
  built and mounted from an explicit call, the same as a domain package's `Commands()`.
- `internal/app` — the composition root; see above.

## Naming

- A root subcommand is bare, never capability-prefixed: `demo` is already the capability word, so
  a scenario under it needs only its own name (`demo domain`, not `demo domain-crud`); `org` is
  the organization domain's own word, and `admin database` nests the admin mount's one domain
  under its own container the same way, so a second admin domain arrives without renaming the
  first.
- A step's Go method name is its `Intent` in lowerCamel, articles and prepositions dropped, a
  nominalized title becoming the verb it nominalizes (`"Results"` → `results`, `"Catalog
  registration"` → `registerCatalog`).
- A direct command's own flags name the wire field they build (`--code`, `--parent-id`); `--body`
  and `--version` are the two names every body-taking and guarded command shares, never
  reinterpreted per command.

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

## Direct commands

A direct command (`org`, `admin database`) sends one real request and prints its result — distinct
in kind from a narrated scenario's tour, which shows what it is about to do before doing it. One
subcommand per route, matched 1:1 (`org get-by-path`, `admin database schema force`); a
zero-input route uses `output.FixedCommand`, everything else builds its request from
`input.Body`/`input.GuardedBody` and renders it through `output.Response`/`output.Expect`. A
domain's `Commands(newClient func() *Client)` never names `httpx.NewClient` itself — the
composition root closes `newClient` over the base URL cobra parses during `Execute`, so
construction happens the first time a subcommand actually runs, never when the tree is built.
