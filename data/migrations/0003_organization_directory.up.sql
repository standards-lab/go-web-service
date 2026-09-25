-- The organization's documents at the directory grain: one owner row binds
-- a top-level blobfs directory, the organization's document root, to the
-- organization, and every directory and file beneath it is the
-- organization's by containment, checked once at that ancestor. An
-- organization has at most one root. The foreign key into blobfs_directory
-- refuses removing an owned directory while its owner row stands, so the
-- removal deletes the row in the same transaction; the one into
-- organization refuses deleting an organization that still owns a root.
CREATE TABLE organization_directory (
  directory_id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_organization_directory_directory FOREIGN KEY (directory_id) REFERENCES blobfs_directory (id),
  CONSTRAINT fk_organization_directory_organization FOREIGN KEY (organization_id) REFERENCES organization (id),
  CONSTRAINT uq_organization_directory_organization UNIQUE (organization_id)
);
