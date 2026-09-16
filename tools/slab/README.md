# slab

`slab` is a narrated-scenario runner for the go-web-service reference architecture. Each scenario
says what it is about to do, does it — against the real running stack, or, for `sqlate`, in
process over the repository's own sources — and prints what it observed. It is not a v1 layer of
the service itself: it is the architect's own instrument for briefing colleagues and leadership
on the workspace's progress, and a self-serve path in for anyone exploring it themselves.

It is its own Go module, rooted here, so cobra and the scenario machinery never enter
`cmd/server`'s dependency graph. Anyone with `go-web-service` cloned already has it.

## Running it

From the repository root:

```sh
mise run slab -- list                 # every registered scenario, its summary, and what it needs
mise run slab -- demo sqlate          # the compile pipeline, in process (no compose stack needed)
mise run slab -- demo domain          # the organization domain's full CRUD, against the running service
mise run slab -- demo problems        # a tour of the service's problem responses, against the running service
```

`demo domain` and `demo problems` need the full compose stack:

```sh
mise run db-up && mise run otel-up
mise run serve   # in another shell
```

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
