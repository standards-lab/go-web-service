--| tier: standard
-- Binds a pending file to the organization as an inactive image, in the
-- transaction that begins the file's write. A nonexistent organization fails
-- fk_organization_image_organization, a file already bound
-- uq_organization_image_file.
INSERT INTO organization_image (organization_id, file_id)
VALUES ({{organization_id:uuid}}, {{file_id:uuid}})
