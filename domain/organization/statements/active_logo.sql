--| tier: standard
-- The organization's active logo: the blobfs file its active image binds,
-- whatever the file's status, in the column order blobfs.File scans. The
-- partial unique index ux_organization_image_active admits at most one
-- active row.
SELECT {{> blobfs.file_columns}}
FROM organization_image i
JOIN blobfs_file f ON f.id = i.file_id
WHERE i.organization_id = {{organization_id:uuid}} AND i.active
