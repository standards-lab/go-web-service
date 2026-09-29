--| tier: standard
-- Binds the organization's top-level directory as its document root, in the
-- transaction that ensures the directory. A second root for the organization
-- fails uq_organization_directory_organization; a directory already bound
-- fails the primary key.
INSERT INTO organization_directory (directory_id, organization_id)
VALUES ({{directory_id:uuid}}, {{organization_id:uuid}})
