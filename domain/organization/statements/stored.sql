--| tier: standard
-- The organization as the command that changed it stored it, read in that
-- command's transaction for its event: the stored fields alone, under the
-- event's names, without the read model's lineage or timestamps.
SELECT id AS organization_id, parent_id, code, name, version
FROM organization
WHERE id = {{id}}
