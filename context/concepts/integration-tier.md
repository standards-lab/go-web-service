# The integration tier's next steps

What the integration tier (the root `integration` package, built at `v1.data.sql.tasks.suite`,
2026-09-07) grows next. The tier itself is expressed by the code and the README; this note
carries the direction for the two roadmap tasks that follow it and one later convention, at
claim resolution until each task's session settles it. The decision record for the tier is
standards-lab `context/design/testing-hierarchy.md`.

## The toolkit (`v1.data.sql.tasks.toolkit`)

The harness was written with its promotion seams visible, one file each, and the sufficiency
rule now has its first consumer. The pieces move to the layer that owns what they exercise:

- The process runner (`Main`, `Launch`, `Ready`, `Stop`, the output capture, the race runtime's
  exit-sleep setting, the interrupt-then-kill cleanup) to go-core beside `process`, which owns
  the signal contract it drives.
- The client, the problem decoding, and the `IfMatch` header to a `go-web-sdk/webtest` package,
  beside the SDK that owns the problem format and the precondition.
- The forwarder is generic: a TCP relay for any backing service, the database its first tenant.
  Its home is decided by the second consumer; it may stay with the runner.
- The template gains the wiring: an `integration` package with the boot, probe, and drain suite
  over the toolkit, the `integration` mise task on an isolated compose project, and the CI job,
  all engine-free.
- The service's harness thins to the toolkit; `state.go` stays, since state control is the
  service's own.

The convention the task records in the testing hierarchy: a library whose infrastructure is
exercised by integration testing ships its integration toolkit beside it, the way `sqltest`
ships beside sqlate for the unit tier.

Assumes the toolkit's API is taken from the harness as built, not redesigned; a second service
consumer is the point at which it is revisited.

## Named database states (`v1.data.sql.tasks.states`)

The suite resets state through the admin mount's schema verbs and the one seed set, and the
development database is test tooling that should reach any state a scenario needs without a
sequence of hand steps. The feature generalizes the seeder: named sets declared as data, a
transition in go-database's admin service that resets the schema and applies a named set,
exposed through the admin mount and a startup switch, with `mise run db-state <name>` for the
developer and the suite's `Reset` becoming one call to the same endpoint. The library half is
go-database's; the sets are the service's.

Assumes the transition composes the existing migrate and seed mechanisms rather than adding a
third; the session that builds it settles the declaration format.

## Per-domain protocol helpers (at the second domain)

The organization suite already shows the shape every domain repeats: the guarded-command ladder
(428, malformed If-Match 400, stale 412, absent 404, then the 200 that advances the version)
and the collection read's paging, sort, and filter 400s, both owned by the SDK and the
read-model header rather than the domain. When `v1.data.tasks.people` lands, the second
instance is the moment to extract a `GuardedCommand` and a `CollectionRead` assertion helper,
leaving each domain's file its own invariants. Not before: one instance is a guess.
