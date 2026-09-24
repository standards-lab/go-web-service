--| tier: standard
-- Removes the image binding file_id, in the transaction that begins the
-- file's delete, so the purge meets no reference through
-- fk_organization_image_file.
DELETE FROM organization_image
WHERE file_id = {{file_id:uuid}}
