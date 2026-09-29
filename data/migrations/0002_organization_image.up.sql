-- The organization's images at the file grain: one row per blobfs file an
-- organization holds, and at most one active per organization, which a
-- partial unique index guards (a native form; README, Stack). A file is
-- bound once. The foreign key into blobfs_file is the backstop blobfs's
-- purge step reports as its referenced sentinel, and the one into
-- organization refuses deleting an organization that still holds an image.
CREATE TABLE organization_image (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  organization_id uuid NOT NULL,
  file_id uuid NOT NULL,
  active boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_organization_image_organization FOREIGN KEY (organization_id) REFERENCES organization (id),
  CONSTRAINT fk_organization_image_file FOREIGN KEY (file_id) REFERENCES blobfs_file (id),
  CONSTRAINT uq_organization_image_file UNIQUE (file_id)
);

CREATE UNIQUE INDEX ux_organization_image_active ON organization_image (organization_id) WHERE active;
