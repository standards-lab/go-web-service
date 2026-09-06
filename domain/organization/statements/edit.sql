--| tier: standard
UPDATE organization
SET code = {{code}}, name = {{name}}, {{> sql.guard_set}}
WHERE {{> sql.guard_where}}
