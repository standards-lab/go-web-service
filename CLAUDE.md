# go-web-service

The reference web service of Go Minimal, the Standards Lab organization's minimal-dependency Go
standard: a production web service composed from go-core, go-web-sdk, and go-database on the
go-web-sdk-template baseline, on one declared stack (Postgres). Managed with the marathon
workflow; start from `context/README.md`.

## Role boundary

go-web-service is a marathon **code** project (`.claude/marathon.toml` declares
`kind = "code"`). The developer owns the production Go source — they apply it and answer for
it. The agent writes everything else: tests, godoc and `doc.go`, prose documentation, the files
in `context/`, the implementation guide, and the reset file.

## Repository specifics

- **Module layout.** One module at the root, `github.com/standards-lab/go-web-service`, with
  two binaries: `cmd/server` (the service) and `cmd/db` (database operations). The composition
  root is `internal/app` over the `internal/{infrastructure,domain,reactors}` layers;
  `internal/process` holds the pre-infrastructure main-sequence parts both binaries share.
- **Dependencies.** go-core, go-web-sdk, go-database, and go-database/postgres at pinned
  releases, plus golang-migrate, on Go 1.27. The pins are the committed steady state; a
  gitignored local `go.work` serves sibling development.
- **Stack.** Postgres is the declared SQL engine, run locally through `compose.yml`. A provider
  variant is never a switch inside this service; it would be a separate focused reference.
- **Documented layers.** The documented layer is the unit of change — each capability lands
  complete before the next begins (`context/design/documented-layers.md`). The service is
  versionless until its first release; no tags, no `CHANGELOG.md` yet.
- **Tests.** Hermetic by default: no live database in the suite. `internal/config/configtest`
  is the single source of valid test configuration — a new subsystem's required fields are set
  there once. Database-backed proof runs against the compose stack.
- **Public repo.** This repository is public on GitHub.
