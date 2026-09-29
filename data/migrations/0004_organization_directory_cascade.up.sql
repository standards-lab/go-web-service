-- The owner row goes with the directory it binds: removing a document root,
-- by the plain delete of an empty root or by the sweep of a recursive
-- delete, deletes its organization_directory row in the same statement.
-- The cascade is the consumer's own foreign key, which blobfs leaves to the
-- consumer: its no-cascade rule covers its own keys, where a cascade would
-- remove file rows whose objects still exist. An owner row holds no
-- object, so cascading it strands nothing.
ALTER TABLE organization_directory
  DROP CONSTRAINT fk_organization_directory_directory,
  ADD CONSTRAINT fk_organization_directory_directory FOREIGN KEY (directory_id) REFERENCES blobfs_directory (id) ON DELETE CASCADE;
