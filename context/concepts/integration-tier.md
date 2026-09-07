# The integration tier's next steps

What the integration tier (the root `integration` package, built at `v1.data.sql.tasks.suite`,
2026-09-07, thinned to the SDKs' toolkit at `v1.data.sql.tasks.toolkit` the same day, and given
named states as its state control at `v1.data.sql.tasks.states`, also the same day) grows next.
The tier itself is expressed by the code and the README; this note carries one later
extraction, at claim resolution until a session settles it. The decision record for the tier is
standards-lab `context/design/testing-hierarchy.md`.

## Per-domain protocol helpers (at the second domain)

The organization suite already shows the shape every domain repeats: the guarded-command ladder
(428, malformed If-Match 400, stale 412, absent 404, then the 200 that advances the version)
and the collection read's paging, sort, and filter 400s. Both are the SDK's and the read-model
header's own, not the domain's, so a `GuardedCommand` and a `CollectionRead` assertion helper
belong in go-web-sdk's `webtest` when they are extracted, leaving each domain's file its own
invariants. The criterion is fit, not a count of consumers (standards-lab
`design/service-organization.md`); `v1.data.tasks.people` is the session that extracts them
because that is when a second suite file would otherwise repeat them.
