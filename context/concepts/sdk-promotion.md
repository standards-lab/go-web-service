# SDK promotion inventory

The library candidates staged in the base `sdk` package during `v1.data.reads.organization`
(2026-08-26), held for consolidation at the writes planning session — each library lands its
staged candidates and its write vocabulary as one release, so the API is designed once with
both slices of evidence. `v1.data.evaluation` remains the final audit. The layering rationale
is `design/domain-architecture.md`; this note is the working inventory and decays as the
consolidation lands.

## → go-web-sdk (`sdk/web.go`)

- **`ParseRead`** — the fused read-contract parse: directives plus the filter remainder from
  one call, the reserved parameter set private to it. Likely absorbs or siblings
  `ParseDirectives`.
- **`Status` / `WriteError`** — the domain-error-to-problem mapping the SDK's direction note
  has been waiting on a consumer for, with the detail rule: error text only on a 400; no
  internal sentinel's text on the wire. Promotion must resolve the go-database coupling —
  `Status` matches `query` errors, so the mapping either splits by source or promotes as a
  matcher seam the consumer composes (personnel-service-demo's `statusMatchers` is prior art).

## → go-database (`sdk/database.go`)

- **`Columns` / `Fields`** — structural builders for select lists and projected fields.
- **`Scan[T]` and the executors** — `SelectList`, `SelectOne`, `SelectPage`: the live test of
  the library's no-execution-helper decision, now proven by a consumer.
- **The one-row read as a `Projection` operation** — `SelectOne` currently reassembles the
  select list and resolves field names consumer-side (`columns`/`fieldExpr`), deliberate
  staging debt; a library-level single-row statement keeps resolution and the typed
  unknown-field error where they belong and deletes the duplication.
- **Operation-shaped constructors as the package idiom** — `Statements` is really the list
  operation; the one-row read wants to be its sibling on `Projection`, and the writes slice
  designs its command shapes (insert returning identity+version, guarded update, guarded
  delete) on the same idiom, each with its own executor and typed error model.
- **`RecursivePath`** — the computed-field pattern tier: an adjacency-list path composition
  declared as a spec value, separator and rootedness parameterized (URL paths and dotted
  slugs both render). It returns the whole projection because the CTE replaces the FROM and
  already requires every projection input; a consumer composing *several* computed fields
  decomposes it into projection transformers each contributing a CTE and field — a recorded
  seam for the promotion design, not built for one pattern with one consumer.

## Template implications (go-web-sdk-template)

For the evaluation task or an earlier coordinator sweep:

- `routes` widens to `routes(dom *domain.Domain, cfg *config.Config)`.
- Its body ships the initialized empty `/api` module, modeling the convention at the edit
  point.
- The `internal/domain` doc comment says defined in base packages, constructed here.

## Timing

Everything above waits for the writes planning session; nothing promotes mid-slice. After the
writes features are locked in, a holistic operation pass over the whole architecture
re-evaluates every layer — including these promotions as landed — end to end.
