--| tier: standard
-- Clears the organization's active image, if it has one, ahead of
-- activating its replacement in the same transaction.
UPDATE organization_image
SET active = FALSE
WHERE organization_id = {{organization_id:uuid}} AND active
