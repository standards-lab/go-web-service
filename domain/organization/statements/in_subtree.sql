--| tier: standard
-- Whether candidate sits in node's subtree, node itself included: walk
-- candidate's ancestor chain upward and count node among it. The anchor row
-- makes a self-parent a cycle without a separate check.
WITH RECURSIVE ancestor (id, parent_id) AS (
    SELECT o.id, o.parent_id FROM organization o WHERE o.id = {{candidate:uuid}}
  UNION ALL
    SELECT o.id, o.parent_id FROM organization o JOIN ancestor a ON o.id = a.parent_id
)
SELECT COUNT(*) FROM ancestor WHERE id = {{node:uuid}}
