--| tier: native
--| native: postgres — ON CONFLICT DO NOTHING and RETURNING. Ports: MERGE (SQL Server, Oracle), INSERT IGNORE + a second read (MySQL).
-- Inserts one organization unless a sibling already carries the code, in
-- which case no row returns and the seeder finds the existing one.
INSERT INTO organization (parent_id, code, name)
VALUES ({{parent:uuid}}, {{code}}, {{name}})
ON CONFLICT ON CONSTRAINT uq_organization_parent_code DO NOTHING
RETURNING id
