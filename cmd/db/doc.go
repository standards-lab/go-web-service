// The database operations binary, one concern per file:
//
//   - main.go     command dispatch
//   - migrate.go  withInfrastructure — the construction every command
//     shares: config.Load, infrastructure.New with a nil coordinator, the
//     database's Start, and its deferred Shutdown, so the binary inherits
//     the configured logger, pool, and provider and drives connectivity
//     directly — and the migrate verbs over golang-migrate. newMigrator is the
//     construction convention: the iofs source over the embedded files and
//     the pgx/v5 driver wrapped around the shared pool by explicit instance
//     construction, no driver registry. noChangeOK is the semantics
//     convention: an already-current schema is success, so reruns are
//     idempotent
//   - seed.go     the seed command: the library's seed runner given what only
//     this service knows — the embedded seed files, the organization loader
//     with its conflict target, and the step order
//   - embed.go    the embedded migration and seed file systems
//
// Migration files are cmd/db/migrations/NNNN_<slug>.{up,down}.sql, each a
// single implicit transaction — no explicit COMMIT, nothing non-transactional
// — so a failed migration rolls back atomically. Exit codes follow
// internal/process: 0 ok, 1 runtime failure, 2 usage error.
package main
