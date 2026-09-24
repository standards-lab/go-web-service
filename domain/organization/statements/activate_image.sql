--| tier: standard
-- Makes the image binding file_id its organization's active one. A
-- concurrent replacement that activated another image first fails the
-- partial unique index ux_organization_image_active.
UPDATE organization_image
SET active = TRUE
WHERE file_id = {{file_id:uuid}}
