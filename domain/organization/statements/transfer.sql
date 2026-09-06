--| tier: standard
UPDATE organization
SET parent_id = {{parent_id:uuid}}, {{> sql.guard_set}}
WHERE {{> sql.guard_where}}
