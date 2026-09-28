--| tier: standard
-- The organization a root is ensured for, read before the root's directory
-- is written, so a nonexistent organization is the missing row rather than
-- the owner row's foreign-key violation.
SELECT id
FROM organization
WHERE id = {{organization_id:uuid}}
