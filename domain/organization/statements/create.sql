--| tier: native
--| native: postgres — RETURNING. Ports: OUTPUT INSERTED (SQL Server), RETURNING INTO (Oracle), LAST_INSERT_ID + a read (MySQL).
INSERT INTO organization (parent_id, code, name)
VALUES ({{parent_id:uuid}}, {{code}}, {{name}})
{{> app.identity}}
