# Organization lineage

The org path (`/acme/engineering/platform`) is a read-model projection composed at read time from the
parent chain in a recursive CTE (`domain/organization/statements/organization_view.sql`); nothing stores
it, and sibling-scoped unique codes are what make every composed path unique. `path` stays a projected
field; the CTE materializes per statement, so every organization read walks the tree.

## Materializing the lineage

Unit-scoped grants need "is unit X under unit U?" on every authorized request, so a recursive walk per
authorization check is the wrong cost model. `organization_closure(ancestor_id, descendant_id, depth)` is
the closure table that answers it: an ancestor test becomes an indexed join, at the cost of maintaining
the table alongside every create, transfer, and delete. The reasoning against the alternatives (`ltree`,
a materialized text path) is in standards-lab `context/design/auth-strategy.md` §5 and §10.

Maintenance runs as standard SQL inside the transaction the guarded transfer command already runs, under
the advisory lock that already serializes it: create inserts the self row plus a copy of the parent's
ancestor rows at depth+1; transfer deletes the moved subtree's former ancestor links and inserts the
cross product of the new parent's ancestors-and-self with the moved subtree's descendants-and-self;
delete needs nothing beyond the node's own closure rows, since the schema already blocks deleting a node
with children.

Composing `path` from the closure table instead of the recursive CTE — retiring the per-read tree walk —
is an available follow-on, not yet built.
