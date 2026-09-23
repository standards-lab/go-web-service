# Integration tier

What the integration tier, the root `integration` package, grows next. The code and the README
express the tier itself.

## Per-domain protocol helpers (at the second domain)

The organization suite already shows the shape every domain repeats: the guarded-command ladder
(428, malformed If-Match 400, stale 412, absent 404, then the 200 that advances the version)
and the collection read's paging, sort, and filter 400s. Both are the SDK's and the read-model
header's own, not the domain's, so a `GuardedCommand` and a `CollectionRead` assertion helper
belong in go-web-sdk's `webtest` when they are extracted, leaving each domain's file its own
invariants. The criterion is fit, not a count of consumers; `v1.data.people` extracts them,
because its suite file would otherwise repeat them.
