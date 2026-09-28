ALTER TABLE organization_directory
  DROP CONSTRAINT fk_organization_directory_directory,
  ADD CONSTRAINT fk_organization_directory_directory FOREIGN KEY (directory_id) REFERENCES blobfs_directory (id);
