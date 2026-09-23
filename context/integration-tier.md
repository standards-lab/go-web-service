# Integration tier

What the integration tier, the root `integration` package, adds next. The code and the README
describe the tier as it is.

## Per-domain protocol helpers (planned for the second domain)

Two assertion helpers, `GuardedCommand` and `CollectionRead`, are planned for go-web-sdk's
`webtest` package. The organization suite already shows the two sequences every domain
repeats: the guarded-command ladder (428, malformed If-Match 400, stale 412, absent 404, then
the 200 that advances the version) and the collection read's paging, sort, and filter 400s.
Both sequences belong to the SDK and the read-model header, not to the domain, so the helpers
belong in `webtest` and leave each domain's suite file with only its own invariants. The
criterion for extracting them is fit, not a count of consumers; `v1.data.people` extracts them,
because its suite file would otherwise repeat them.
