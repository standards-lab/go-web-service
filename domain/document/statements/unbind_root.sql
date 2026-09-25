--| tier: standard
-- Removes the owner row of a document root, in the transaction that removes
-- the directory, so the removal meets no reference through
-- fk_organization_directory_directory.
DELETE FROM organization_directory
WHERE directory_id = {{directory_id:uuid}}
