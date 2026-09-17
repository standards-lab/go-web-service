# slab

`slab` calls go-web-service's own endpoints directly, one subcommand per route under `org` and
`admin`, and runs narrated scenarios under `demo`: each scenario says what it is about to do, does
it — against the real running stack, or, for `sqlate`, in process over the repository's own
sources — and prints what it observed. The direct commands are a scriptable replacement for
ad-hoc curl; the scenarios are the architect's own instrument for briefing colleagues and
leadership on the workspace's progress, and a self-serve path in for anyone exploring it
themselves.

It is its own Go module, rooted here, so cobra and its command machinery never enter
`cmd/server`'s dependency graph. Anyone with `go-web-service` cloned already has it.

[`usage.md`](usage.md) walks through every command by hand against the real running stack.

## Running it

From the repository root:

```sh
mise run slab -- list                            # every scenario, its summary, and what it needs
mise run slab -- demo sqlate                     # the compile pipeline, in process (no compose stack needed)
mise run slab -- demo domain                     # the organization domain's full CRUD, against the running service
mise run slab -- demo problems                   # a tour of the service's problem responses, against the running service
mise run slab -- org list                        # the organization domain's endpoints, one subcommand per route
mise run slab -- admin database schema status    # the admin mount's endpoints, under their own domain word
```

Everything but `demo sqlate` needs the full compose stack:

```sh
mise run db-up && mise run otel-up
mise run serve   # in another shell
```

## Commands

`org` and `admin` call the running service's endpoints directly, one subcommand per route,
printing the response as pretty JSON — or, for an empty body such as `org delete`'s 204, the
status line alone, so a command is never silent. This is distinct in kind from a `demo`
scenario's narrated tour: a command sends one real request and returns its result.

- **org** — the organization domain's seven endpoints: `list` (`--page`, `--size`, `--sort`, and a
  repeatable `--filter field=value` or `field[op]=value`), `get <id>`, `get-by-path <path>`,
  `create`, `edit <id>`, `transfer <id>`, and `delete <id>`. `create`/`edit`/`transfer` take named
  flags (`--code`, `--name`, `--parent-id`) or a `--body <json>` escape hatch sent verbatim,
  mutually exclusive with the flags. `edit`/`transfer`/`delete` need `--version` for the request's
  `If-Match`; on `edit`/`transfer`, a top-level `"version"` in `--body` stands in for the flag, so
  the value isn't given twice. `transfer --parent-id=` moves an organization to the root — the
  flag must be given, empty or not, since the service requires the key present in the body.
- **admin database** — the database admin service's twelve endpoints: `schema status`, `verify`,
  `up`, `down` (`--steps`, optional — one migration when unset), `steps` (`--steps`, required),
  and `force` (`--version`, required) under `schema`; `seed` (`--state`, optional — the service's
  configured set when unset — additive, never a reset: it inserts a set's rows idempotently over
  the schema as it stands, so seeding an empty set onto a populated database changes nothing) and
  `state` (`--state`, required — reverts every migration, reapplies the schema, then seeds; the
  one that actually clears first); and `diagnostics`, `patterns`,
  `statements`, `states`, which take no input at all. Every body-taking command also accepts
  `--body <json>` in place of its flags.

Both share the persistent flags below with `demo`.

## Scenarios

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

## Flags

`--base`, `--grafana`, and `--tempo` name the service's, Grafana's, and Tempo's base URLs (env
`SLAB_BASE`, `SLAB_GRAFANA`, `SLAB_TEMPO`; they default to the compose stack's local ports).
`--repo` names the repository root, when `slab` is not run from inside it. `--no-color` prints
without ANSI color even on a terminal.
