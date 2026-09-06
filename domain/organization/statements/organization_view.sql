--| tier: standard
--| key: id
--| field: id uuid
--| field: parent_id uuid
--| field: code text
--| field: name text
--| field: version bigint
--| field: created_at timestamp with time zone
--| field: updated_at timestamp with time zone
--| field: path text
-- The read model: every organization with its path composed from the
-- lineage, never stored. The SELECT list is the scan order.
WITH RECURSIVE lineage (id, parent_id, code, name, version, created_at, updated_at, path) AS (
    SELECT o.id, o.parent_id, o.code, o.name, o.version, o.created_at, o.updated_at,
           '/' || o.code
    FROM organization o
    WHERE o.parent_id IS NULL
  UNION ALL
    SELECT o.id, o.parent_id, o.code, o.name, o.version, o.created_at, o.updated_at,
           l.path || '/' || o.code
    FROM organization o
    JOIN lineage l ON l.id = o.parent_id
)
SELECT id, parent_id, code, name, version, created_at, updated_at, path
FROM lineage
