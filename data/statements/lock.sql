--| tier: native
--| native: postgres — pg_advisory_xact_lock over hashtext. Ports: sp_getapplock (SQL Server), GET_LOCK (MySQL), DBMS_LOCK (Oracle), each taking the name directly, or a FOR UPDATE mutex row.
--| transaction: required
-- Takes the named advisory lock for the rest of the transaction. The name
-- is one of the lock registry's, and the engine hashes it.
SELECT pg_advisory_xact_lock(hashtext({{name}}))
