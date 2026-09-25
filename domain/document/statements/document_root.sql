--| tier: standard
-- The organization's document root: the top-level directory its owner row
-- binds, or no row before the organization's first write. Every scope check
-- reads it first, so an id outside the root reveals nothing.
SELECT directory_id
FROM organization_directory
WHERE organization_id = {{organization_id:uuid}}
