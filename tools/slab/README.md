# slab

`slab` calls go-web-service's own endpoints directly, one subcommand per route under `org`,
`docs`, and `admin`, watches the events the service publishes under `events`, and runs narrated scenarios under `scenario`: each scenario says what it
is about to do, does it — against the real running stack, or, for `sqlate`, in process over the
repository's own sources — and prints what it observed. The direct commands are a scriptable
replacement for ad-hoc curl; the scenarios are the architect's own instrument for briefing
colleagues and leadership on the workspace's progress, and a self-serve path in for anyone
exploring it themselves.

It is its own Go module, rooted here, so its command machinery never enters `cmd/server`'s
dependency graph: go-cli-sdk, go-messaging's NATS provider and nats.go, which `events watch`
reads the service's stream through, and the `golang.org/x/term` and `golang.org/x/sys` modules the
`style` package detects a terminal with. Anyone with `go-web-service` cloned already has it.

[`usage.md`](usage.md) walks through every command by hand against the real running stack.

## Running it

From the repository root:

```sh
mise run slab -- scenario                        # every scenario, its summary, and the nodes it uses (as does -- --help)
mise run slab -- scenario sqlate                 # the compile pipeline, in process (no compose stack needed)
mise run slab -- scenario domain                 # the organization domain's full CRUD, against the running service
mise run slab -- scenario problems               # a tour of the service's problem responses, against the running service
mise run slab -- scenario storage                # logos, documents, the refusals, and the sweep, against the running service
mise run slab -- scenario events                 # one command per event the service publishes, for events watch to print
mise run slab -- scenario sweeper                # the general sweeper's reclaimers through their metrics in Mimir
mise run slab -- org list                        # the organization domain's endpoints, one subcommand per route
mise run slab -- docs dirs list <org-id> root    # the document domain's endpoints, grouped by dirs and files
mise run slab -- admin database schema status    # the admin mount's endpoints, under their own domain word
mise run slab -- events watch                    # each event the service publishes, live, until Ctrl-C
mise run slab -- version                         # slab's version
```

Everything but `scenario sqlate` needs the compose stack and the service (`scenario storage`
and `scenario events` read no traces, so they need no Grafana, but `mise run serve` streams its
logs to the collector; `scenario sweeper` reads Mimir through Grafana, so it needs the
observability profile up and the service exporting to it; `events watch` needs only NATS and the stream the service creates when it
starts):

```sh
mise run db:up && mise run otel:up
mise run serve   # in another shell
```

## Commands

`org`, `docs`, and `admin` call the running service's endpoints directly, one subcommand per
route, printing the response as pretty JSON — or, for an empty body such as `org delete`'s 204,
the status line alone, so a command is never silent. A problem response prints nothing on
stdout and one report on stderr: `slab <command path>: ` and the status line, then the detail,
the instance, the type, and each extension member, one per line. A command sends one real
request and returns its result, unlike a scenario's narrated tour.

- **org** — the organization domain's ten endpoints: `list` (`--page` or `--cursor`, `--size`,
  `--sort`, and a repeatable `--filter field=value` or `field[op]=value`), `get <id>`,
  `get-by-path <path>` (the service's `lookup?path=`), `create`, `edit <id>`, `transfer <id>`,
  `delete <id>`, and the three `logo` commands. `create`/`edit`/`transfer` take named flags
  (`--code`, `--name`, `--parent-id`) or a `--body <json>` escape hatch sent verbatim, mutually
  exclusive with the flags. `edit`/`transfer`/`delete` need `--version` for the request's
  `If-Match`; on `edit`/`transfer`, a top-level `"version"` in `--body` stands in for the flag, so
  the value isn't given twice. `transfer --parent-id=` moves an organization to the root — the
  flag must be given, empty or not, since the service requires the key present in the body.
  `logo put <id> <file>` sends a file's bytes as the logo, with the media type from the
  extension unless `--content-type` names one. `logo get <id>` prints the object headers and
  writes the bytes to `--out`, never to the terminal; `--if-none-match <etag>` revalidates, and
  the service answers 304 when the logo is unchanged. `logo delete <id>` removes the logo.
- **docs** — the document domain's eleven endpoints. Every command takes an organization id
  `<org>` first, and a directory `<dir>` is an id or the `root` alias. `dirs` holds
  `create <org>` (`--parent-id <dir>`, `--name`), `get <org> <dir>` (the metadata with the path
  and the `status`, `active` or `deleting`), `list <org> <dir>` (the child directories, under
  `org list`'s paging and filter flags), `move <org> <dir>` (`--version`, `--parent-id`,
  `--name`), and `delete <org> <dir>` (`--version`). `dirs delete` answers 204 for an empty
  directory; `--recursive` instead marks the whole branch deleting and prints the 202's
  `Location` to read while the service's sweep removes the branch. `--wait <duration>` then
  polls that `Location` until it answers 404, and fails if the duration runs out first. `files`
  holds `list <org> <dir>` (the directory's files, under the same flags),
  `put <org> <dir> <file>`, `show <org> <file-id>` (the metadata), `get <org> <file-id>` (the
  headers, `Content-Disposition` among them, with the bytes to `--out` and `--if-none-match` as
  on `logo get`),
  `move <org> <file-id>` (`--version`, `--directory-id`, `--name`), and `delete <org> <file-id>`
  (`--version`). `files put` uploads with the service's `POST …/files?name=…` and stores the
  file under its base name unless `--name` gives one; its
  media type is `--content-type`, else the extension's, else `application/octet-stream`, since
  the service accepts any. Both deletes need `--version` for the request's `If-Match`, as
  `org delete` does; the service answers 428 without it and 412 when it is stale. `dirs create`
  and both moves take `--body` in place of their field flags, and on a move a top-level
  `"version"` in the body stands in for `--version`, as on `org edit`.
- **admin database** — the database admin service's twelve endpoints: `schema status`, `verify`,
  `up`, `down` (`--set`, required; `--steps`, optional — one migration when unset), `steps`
  (`--set` and `--steps`, required), and `force` (`--set` and `--version`, required) under
  `schema`, the set being one of the migration sets `schema status` lists; `seed` (`--state`,
  optional — the service's configured set when unset — additive, never a reset: it inserts a
  set's rows idempotently over the schema as it stands, so seeding an empty set onto a populated
  database changes nothing) and `state` (`--state` and `--confirm`, required — reverts every
  migration set, reapplies them, then seeds; the one that actually clears first); and
  `diagnostics`, `patterns`, `statements`, `states`, which take no input at all. Every
  body-taking command also accepts `--body <json>` in place of its flags.
- **admin storage** — the object store's admin endpoints: `diagnostics` (whether a live probe
  succeeds, the container, and the key length bound) and `container` (creates the configured
  container, succeeding when it exists). Neither takes input.
- **events** — `events watch` prints each event the service publishes to its NATS JetStream
  stream as it arrives, until interrupted: the type, then the id, subject, and time as a table,
  then the data, as indented JSON when it is JSON, and a blank line. It starts by naming on stderr
  the stream and subjects it watches, so stdout carries the events alone. By default it shows
  only the events published after it starts; `--from-start` replays every event the stream holds
  first, then follows. It connects through go-messaging's NATS broker as a non-owner, binding the
  stream the service provisioned and writing nothing to it, and reads it with JetStream's ordered
  consumer, ephemeral and unacknowledged and filtered on `<prefix>.>`, which it deletes when it
  ends, so it settles nothing and leaves no consumer behind. Ctrl-C is its clean end: exit code
  0. A message that does not decode as a CloudEvent, or an error the consumer survives such as
  missed heartbeats while NATS is away, is reported on stderr and the watch goes on; the client
  reconnects on its own. With nothing answering it exits 1 naming `mise run db:up`, as in
  `slab events watch: startup: broker: nothing answers at nats://127.0.0.1:4222 (start the stack
  with: mise run db:up): ...`; with NATS up but no stream, as before the service's first start,
  it names `mise run serve`. The `--nats*` root flags under [Configuration](#configuration) name
  the server, the stream, and the prefix.
- **scenario** — `scenario <name>` runs one of the [scenarios](#scenarios) below on go-cli-sdk's
  scenario runner, narrating each step to stdout. `scenario` alone prints its help, which ends
  with the scenario listing, as the root's help does under `Scenarios:`. Neither reads any
  configuration.
- **version** — prints slab's version: the module version the binary was built at, or `(devel)`
  under `go run`. It reads no configuration.

Every command accepts the root flags listed under [Configuration](#configuration).

## Scenarios

Each scenario lives in the `scenarios` package and runs as `slab scenario <name>`. The listing
gives each one's name, its summary, and a `uses` line naming the graph nodes it declares, such
as `uses service, grafana up, repo, no color`: a scenario builds only those, so `sqlate` reads
nothing but the repository root and the color setting.

- **sqlate** — one pattern, one statement that includes it, the two calls that register them
  against a catalog, and the compiled result: `sqlate`'s own mechanism, run directly against the
  repository's `data/patterns` and `domain/organization/statements` sources. No compose stack
  needed.
- **domain** — the organization domain's full CRUD surface against the running service:
  initialization (reset to the seeded reference tree), a raw list, a filtered/paged list, find,
  find by path, create (with the request's trace pointer into Grafana), edit, transfer, a second
  list showing what the writes did, and delete.
- **problems** — a tour of the service's RFC 9457 problem-response contract: eleven real error
  conditions — a malformed path id, a malformed query, a malformed or oversized body, a domain
  validation failure, a missing or malformed `If-Match`, a stale version, a not-found, a
  duplicate code, and a transfer cycle — one request each, every response's trace pointer
  located in Grafana. Resets to the seeded tree first; none of the conditions write a row, so a
  run never drifts the seed data.
- **storage** — the service's stored files end to end. The scenario reads the two migration sets
  (`blobfs` and `app`) from the schema status and seeds the `default` state additively. It reads
  acme's logo, revalidates it by its ETag (304), replaces it with another seeded fixture read
  back under a new ETag, and deletes it. It reads acme's seeded document tree, then walks its
  top-level directories by cursor, one per page. It shows the scoped refusals: a delete with no
  `If-Match` (428) and with a stale one (412), a taken name (409 with its curated detail), and
  acme's directory id asked for under another organization (404). It deletes a run-named branch
  recursively, shows its 202 and its `deleting` read, then waits out the sweep to 404 as
  `dirs delete --wait` does. It ends with a reset to `default` and the counts the reset seeded.
  The additive seed writes back a logo an interrupted run deleted, and the branch's name is
  unique to the run, so the scenario runs twice in a row. Needs postgres, azurite, and the
  service, not Grafana.
- **events** — one command for each event the service publishes, meant to run while
  `mise run slab -- events watch` runs in another terminal, so the watcher prints each event's
  payload; each step names the events it makes the watcher print. It creates a run-named
  organization under acme, edits it, and transfers it to the root; uploads a seeded fixture as
  its logo, replaces it with another (no `logo.deleted` for the replaced one), and deletes it. It
  creates `reports` (which first creates the organization's document root, so two
  `directory.created`), `scratch`, and `empty`, uploads a file into `reports` and one into
  `scratch`, moves `empty` into `reports` as `archive`, renames the `reports` file, deletes it,
  and deletes `archive`; it deletes `scratch` and then the document root (`root`) recursively,
  waiting out each sweep, one `directory.deleted` each for the marked directory alone, so
  `reports` beneath the root emits nothing. Last it deletes the organization, which nothing
  references once its logo and root are gone. Nineteen events in all, every type at least once.
  A run leaves nothing behind, and its names are unique to the run, so it runs twice in a row.
  Needs postgres, azurite, NATS, and the service, not Grafana.
- **sweeper** — the service's general sweeper, read through its metrics in Mimir: the counter
  `sweep.passes` (`sweep_passes_total` by `sweep_reclaimer` and `sweep_outcome`) and the gauge
  `sweep.backlogged` (`sweep_backlogged_ratio`), queried through Grafana's proxy to its Mimir
  datasource. It shows the sweep block as `config.local.json` merges over `config.json` and the
  three reclaimers' passes by outcome (`files`, `outbox`, `inbox`), creates a run-named branch
  under acme with a file in it, deletes it recursively, which nudges `files`, and waits out the
  sweep. It then polls Mimir until an export sampled after the sweep lands (the service exports
  every 60 seconds, the OpenTelemetry SDK's default; the wait gives up after 2m30s) and shows
  each reclaimer's passes in the window: `files` moved, and `outbox` and `inbox` only on their
  interval, which it says when they did: no command names them, but a delivery runs every
  reclaimer that is due, the nudge's included. It narrates the outbox's
  retention, the short local `outbox_age` making the run's own outbox rows the outbox
  reclaimer's work, with the outbox's pass counter, and ends with the PromQL to run in Grafana's
  Explore. The branch is removed by its own delete, so a run leaves no document and runs twice
  in a row. Needs postgres, azurite, NATS, the service, and the observability profile.

Two of the nodes are needs, each checked when the run starts, before any step:

- **service** — the running service at `--base`, probed at `/readyz`. Nothing answering names
  `mise run serve`; a 503, the service up over a database or storage that is not, names
  `mise run db:up`.
- **grafana up** — the running Grafana at `--grafana`, probed at `/api/health`. A failure names
  `mise run otel:up`.

A need that fails stops the run with exit code 1 and one report, which names the first need
that failed, as in `slab scenario domain: startup: service: nothing answers at
http://127.0.0.1:8080 (start the service with: mise run serve): ...`. A step whose result is
not the one it expects stops the run the same way, naming the step by its number and intent:
`slab scenario <name>: step <i> (<intent>): <error>`.

## Configuration

Each setting has a default, an environment variable that overrides it, and, but for the shutdown
budget, a root flag that overrides the variable. A root flag is accepted at any depth — before the
command word, between a parent and its subcommand, or after the leaf — and overrides its variable
whenever it is given, even at its default value. The `--nats*` defaults are the service's base
configuration, `config.json`'s `nats` block, which a test holds them to.

| Flag | Variable | Default | What it sets |
|---|---|---|---|
| `--base` | `SLAB_BASE` | `http://127.0.0.1:8080` | the service's base URL |
| `--grafana` | `SLAB_GRAFANA` | `http://127.0.0.1:3000` | Grafana's base URL |
| `--repo` | `SLAB_REPO` | found from the working directory | the repository root, when `slab` is not run from inside it |
| `--nats` | `SLAB_NATS` | `nats://127.0.0.1:4222` | the NATS server the service publishes its events to |
| `--nats-stream` | `SLAB_NATS_STREAM` | `WEB_SERVICE` | the stream the service publishes its events to |
| `--nats-prefix` | `SLAB_NATS_PREFIX` | `web-service` | the prefix of the service's event types, which `events watch` filters on as `<prefix>.>` |
| `--no-color` | `SLAB_NO_COLOR` | `false` | print without ANSI color even on a terminal |
| — | `SLAB_SHUTDOWN_TIMEOUT` | `10s` | the budget for shutting down what a command built |

Output is colored only on a terminal, and never when `NO_COLOR` is set to anything non-empty. Only
the blocks are colored — a request, a response, SQL, JSON, a table, a trace pointer — and a
scenario's headings, notes, and captions print plain. An SQL block dims comments, colors sqlate's
`{{…}}` markers, and bolds keywords in the code between. JSON that breaks partway prints as it
came, uncolored. Each setting is read only when a command that needs it runs, and help, the
scenario listing, a usage error, and `version` read none. An unreadable variable, such as
`SLAB_NO_COLOR=maybe`, therefore fails only a command that reads it, with exit code 1 and the
variable named.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | the command succeeded, or `events watch` was interrupted |
| 1 | the command failed: a request that could not be sent, a problem response or another unexpected status, a scenario step or need that failed, NATS or its stream not there for `events watch`, or unreadable configuration |
| 2 | a usage error — an unknown command or scenario, an unknown flag, a missing or extra argument, a missing required flag, exclusive flags given together, or input a command refuses before it sends — reported on stderr with the command's usage; or help, printed on `-h`, `--help`, or a parent such as `slab scenario` run alone |

## Conventions

A new scenario, command, or domain follows these rules. Each package's `doc.go` states the rules
that are its own.

- **Its own module.** slab's `go.mod` requires the workspace libraries it uses, go-cli-sdk among
  them, never go-web-service's root module. The service has no release version, so a dependency
  on it would pin a pseudo-version that goes stale in a fresh clone or CI; local cross-module
  work goes through the gitignored `go.work`.
- **The service's layout.** slab mirrors the service's layout. `internal/app` is the composition
  root, one file per layer, and the only package that constructs a dependency or names a mount.
  `cmd/slab` holds process entry and nothing else. `domain/<name>` is the client-side counterpart
  of a service domain package, and `admin/<name>` of an admin service; the two trees are
  siblings, never nested, as in the service. Each package holds the same four files: `doc.go`,
  `entities.go`, `client.go`, and `commands.go`. An admin package's `entities.go` restates
  request bodies only, because its responses print as raw JSON and nothing decodes them. `events`
  watches the stream the service publishes to rather than calling an endpoint, so it is neither:
  a top-level family over the broker node, as the domain and admin families are over the client
  node.
- **Shared packages.** `output` renders every direct command's result, `input` resolves every
  request body, `style` holds all ANSI styling, and `httpx` holds everything about HTTP that is
  not specific to the service.
- **Scenarios.** A scenario is a `scenario.Scenario` in the `scenarios` package, one file each,
  added to `All` in presentation order. It declares the nodes it reads, need nodes first and
  then the settings, and the composition root's `internal/app/scenario.go` hands it those nodes
  and mounts it under `scenario`; a need is a node whose value probes what it names when the run
  starts and names the mise task that fixes it. A scenario constructs no client: it calls the
  service through the client node's client, which the service node's value carries.
- **Direct commands.** Each direct command maps to one route. A command's field flags name the
  wire field they build (`--code`, `--parent-id`), and `--body` and `--version` mean the same on
  every command. A family's `Commands` takes the client node and the `output.Source`, declares
  them with `Use`, returns `[]*cli.Command` for the composition root's one
  `Add(pkg.Commands(...)...)`, and never names `httpx.NewClient` or a stream. Each leaf is a plain
  function of those two, with no type wrapping them; it refuses bad input in `Validate`, before
  anything is built, finishes through `Source.Respond`, and returns any other failure for the
  dispatcher to report.
- **Naming.** A root subcommand is bare, never capability-prefixed: `scenario domain`, not
  `scenario domain-crud`. `admin database` nests the admin mount's one domain under the `admin`
  command, so a second admin domain can arrive without renaming the first.
