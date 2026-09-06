--| tier: standard
-- The guard's check: the row's current version, or no row.
SELECT version FROM organization WHERE id = {{id}}
