# Organization lineage

Captured at the database-migrations closeout (2026-08-14); the `v1.data.reads` shape settled in the
capability-tiers planning session (2026-08-17). The org path (`/acme/engineering/platform`) is a
read-model projection composed at read time from the parent chain; nothing stores it, and the migrations session's
sibling-scoped unique codes are what make every composed path unique.

## Settled for `v1.data.reads`

- `path` is on every read. The organization projection's FROM wraps a recursive query
  (`WITH RECURSIVE`, SQL:1999 — portable) that walks the tree once per statement, and `path` is an
  ordinary projected field, filterable and sortable like any other. This is what makes the library's
  projection admit expression-backed fields, and it costs one whole-table walk per read, which is
  fine at this scale.
- Resolution — path to node — is a filter on that same lineage (`WHERE path = $1`), which is how
  `GET /organizations/path/{path...}` answers. A walk-down query (split the path, descend by code) is
  the optimization if resolution ever becomes hot; it is not needed now.
- The lineage SQL belongs to the organization domain package, the read-side instance of the rule
  that SQL stays with the consumer. Once the reads task lands, this section is expressed by the code and
  goes; the section below stays.

## Held: materializing the lineage

The read-time query has no write-path cost, and the writes task's re-parent command (`v1.data.writes`) stays simple because of
it. What would trigger storing the lineage is not scale but the auth layer: unit-scoped grants need
"is unit X under unit U?" on every authorized request, and a recursive walk per authorization check
is the wrong cost model. When that arrives, three candidates, none chosen yet:

- **`ltree`** — a stored `path ltree` column with a GiST index; ancestor and descendant operators
  (`@>`, `<@`) make subtree checks an index probe. Native to Postgres: `CREATE EXTENSION ltree`, labels
  limited to `[A-Za-z0-9_-]` separated by dots (the slug codes qualify), and an entry in the port
  list. A re-parent rewrites the subtree's paths in the same transaction.
- **Closure table** — `organization_closure(ancestor_id, descendant_id, depth)`; standard SQL,
  subtree and ancestor tests become joins, path composition a join plus `string_agg … ORDER BY
  depth`. Insert copies the parent's ancestor rows; re-parent deletes and reinserts the subtree's.
- **Materialized text path** — the composed string stored per row with a `text_pattern_ops` index;
  prefix `LIKE` gives subtree queries. Standard SQL, the simplest to add, the weakest semantics.

Whichever lands, the read model keeps `path` as a projected field, so the HTTP contract does not move.
The write-path implication — what a re-parent must maintain — is the writes task's to weigh if the choice is
pulled forward.
