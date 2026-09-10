# Organization lineage

Captured at the database-migrations closeout (2026-08-14); the `v1.data.reads` shape settled in the
capability-tiers planning session (2026-08-17). The org path (`/acme/engineering/platform`) is a
read-model projection composed at read time from the parent chain; nothing stores it, and the migrations session's
sibling-scoped unique codes are what make every composed path unique.

The read-side design is built and the code expresses it: `path` is a projected field computed
by a recursive CTE in an authored SQL file (`domain/organization/statements/organization_view.sql`),
and the path lookup is a filter on it. One nuance survives as a note: the CTE materializes per
statement, so every organization read walks the tree; that stays the accepted cost until the
trigger below.

## Held: materializing the lineage

The read-time query has no write-path cost, and the landed transfer command
(`v1.data.writes.organization`) stays simple because of it: a guarded parent_id update behind
an ancestor-walk cycle check, transfers serialized by the tree's advisory lock. Unit-scoped
grants in the auth layer are what would trigger storing the lineage: they need "is unit X under
unit U?" on every authorized request, and a recursive walk per authorization check is the wrong
cost model. When that arrives, three candidates, none chosen yet:

- **`ltree`** — a stored `path ltree` column with a GiST index; ancestor and descendant operators
  (`@>`, `<@`) make subtree checks an index probe. Native to Postgres: `CREATE EXTENSION ltree`, labels
  limited to `[A-Za-z0-9_-]` separated by dots (the slug codes qualify), and an entry in the port
  list. A re-parent rewrites the subtree's paths in the same transaction.
- **Closure table** — `organization_closure(ancestor_id, descendant_id, depth)`; standard SQL,
  subtree and ancestor tests become joins, path composition a join plus `string_agg … ORDER BY
  depth`. Insert copies the parent's ancestor rows; re-parent deletes and reinserts the subtree's.
- **Materialized text path** — the composed string stored per row with a `text_pattern_ops` index;
  prefix `LIKE` gives subtree queries. Standard SQL, the simplest to add, the weakest semantics.

Whichever lands, the read model keeps `path` as a projected field, so the HTTP contract does
not move. The write-path implication (what a transfer must then maintain) lands in the
transfer command when the choice arrives; the edit/transfer command split anticipates exactly
this.
