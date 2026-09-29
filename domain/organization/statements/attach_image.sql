--| tier: standard
-- Binds a completed, held file to the organization as its active image, in
-- the transaction that activates it after removing the image it replaces.
-- A concurrent replacement that activated its image first fails the
-- partial unique index ux_organization_image_active; a nonexistent
-- organization fails fk_organization_image_organization; a file already
-- bound fails uq_organization_image_file.
INSERT INTO organization_image (organization_id, file_id, active)
VALUES ({{organization_id:uuid}}, {{file_id:uuid}}, TRUE)
