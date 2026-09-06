--| tier: standard
DELETE FROM organization
WHERE {{> sql.guard_where}}
