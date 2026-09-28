--| tier: standard
-- One segment of the organization path a seed names: the organization with
-- the code under the parent, the root's parent null. The seed walks the path
-- from its root a segment at a time, so it resolves a path by the
-- organization's own unique key without the organization layer's lineage
-- read.
SELECT id
FROM organization
WHERE parent_id IS NOT DISTINCT FROM {{parent:uuid}} AND code = {{code}}
