--| tier: standard
--| key: id
--| field: id uuid not null
--| field: parent_id uuid
--| field: code text not null
--| field: name text not null
--| field: version bigint not null
--| field: created_at timestamp with time zone not null
--| field: updated_at timestamp with time zone not null
--| field: path text not null
-- The read model: every organization with its path composed from the
-- lineage, never stored. The SELECT list is the scan order. Every field but
-- parent_id is declared not null, as the table holds it, so a sort on any of
-- them continues by cursor; a sort on parent_id pages by number alone.
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
