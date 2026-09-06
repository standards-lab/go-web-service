--| tier: native
--| native: postgres — RETURNING. Ports: OUTPUT INSERTED (SQL Server), RETURNING INTO (Oracle), LAST_INSERT_ID + a read (MySQL).
-- The identity a command returns, the row's key and its version, so every
-- create ends with {{> app.identity}} and reads the same columns.
RETURNING id, version
