--| tier: standard
-- The organization a seed row names, by parent and code, when the seed
-- found it already there; the root's parent is null.
SELECT id
FROM organization
WHERE parent_id IS NOT DISTINCT FROM {{parent:uuid}} AND code = {{code}}
