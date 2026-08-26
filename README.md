# go-web-service

The reference web service of
[Go Minimal](https://github.com/standards-lab/docs/blob/main/standards/go-minimal/index.md), a
minimal-dependency Go implementation of
[Elemental Architecture](https://github.com/standards-lab/docs/blob/main/architectures/elemental-architecture/index.md).
Initialized from the [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## Stack

- SQL: Postgres

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks. The database password
lives in the gitignored secrets layer:

```sh
mise trust && mise install
echo '{"database":{"password":"app"}}' > secrets.json

mise run db-up      # start the local Postgres and wait for health
mise run migrate    # apply the schema migrations
mise run seed       # load the reference data
mise run serve      # run the service
```

The service logs `server ready` on `localhost:8080` (the `local` overlay binds loopback and runs
debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}  — lifecycle and database
```

Ctrl-C drains in-flight requests and exits with `server stopped`.

## Tasks

Each task wraps a plain command, so the repository works without mise:

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet ./...` | Compile-check and vet |
| `mise run serve` | `go run ./cmd/server` | Run the service locally |
| `mise run test` | `go test -race ./...` | Run the tests |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run ./...` | Lint |
| `mise run db -- <args>` | `go run ./cmd/db` | Run a db command |
| `mise run migrate` | `go run ./cmd/db migrate up` | Apply all pending migrations |
| `mise run seed` | `go run ./cmd/db seed` | Load the reference data |
| `mise run db-up` | `docker compose up -d --wait` | Start the local Postgres |
| `mise run db-down` | `docker compose down` | Stop the local Postgres (keep data) |
| `mise run db-reset` | `docker compose down -v` | Stop the local Postgres and drop its data |

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json` — the base file, every knob at its default.
2. `config.<APP_ENV>.json` — the environment overlay; `mise.toml` sets `APP_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging).
3. `secrets.json`, `secrets.<APP_ENV>.json` — gitignored secret layers; the database password
   goes here.
4. `APP_*` environment variables — the final override: the log, server, and shutdown variables,
   the `APP_DATABASE_*` family (`APP_DATABASE_HOST`, `APP_DATABASE_NAME`,
   `APP_DATABASE_USER`, `APP_DATABASE_PASSWORD`, `APP_DATABASE_PORT`, and the pool settings),
   and the reads paging policy (`APP_READS_DEFAULT_SIZE`, `APP_READS_MAX_SIZE`).

Every file is optional — a deployment can run on the base file and environment variables alone,
or on environment variables only.

## License

Apache 2.0 — see [LICENSE](LICENSE).
