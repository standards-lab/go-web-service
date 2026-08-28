# SDK promotion inventory

The library candidates staged in the base `sdk` package during `v1.data.reads.organization`
(2026-08-26). Both library slices have landed — go-database's in `v1.data.writes.database`
(the builders, `Scan` and the executors, the one-row read on `Projection`, the
operation-shaped constructors, `RecursivePath`), go-web-sdk's in `v1.data.writes.web`
(`ParseQuery` and the `ErrorWriter` mapping) — so what remains here is the template's share.
The layering rationale is `design/domain-architecture.md`; this note decays fully when the
template implications land.

## Template implications (go-web-sdk-template)

For `v1.data.writes.template` or an earlier coordinator sweep:

- `routes` widens to `routes(dom *domain.Domain, cfg *config.Config)`.
- Its body ships the initialized empty `/api` module, modeling the convention at the edit
  point.
- The `internal/domain` doc comment says defined in base packages, constructed here.

## Timing

The service's own recomposition on the released libraries is `v1.data.writes.organization`;
the base `sdk` package deletes there. After the writes features are locked in, the holistic
operation pass re-evaluates every layer — including these promotions as landed — end to end.
