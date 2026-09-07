# The integration tier's next steps

What the integration tier (the root `integration` package, built at `v1.data.sql.tasks.suite`,
2026-09-07, and thinned to the SDKs' toolkit at `v1.data.sql.tasks.toolkit` the same day) grows
next. The tier itself is expressed by the code and the README; this note carries the direction
for the roadmap task that follows it and one later extraction, at claim resolution until each
session settles it. The decision record for the tier is standards-lab
`context/design/testing-hierarchy.md`.

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
and the collection read's paging, sort, and filter 400s. Both are the SDK's and the read-model
header's own, not the domain's, so a `GuardedCommand` and a `CollectionRead` assertion helper
belong in go-web-sdk's `webtest` when they are extracted, leaving each domain's file its own
invariants. The criterion is fit, not a count of consumers (standards-lab
`design/service-organization.md`); `v1.data.tasks.people` is the session that extracts them
because that is when a second suite file would otherwise repeat them.
