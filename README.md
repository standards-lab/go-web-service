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
lives in the gitignored secrets layer; copy the committed example to create it. A missing
`secrets.json` is not a load error — the service starts with an empty password and fails only at
the database connection, so this step comes first:

```sh
mise trust && mise install
cp secrets.example.json secrets.json

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

## API

The organization domain is mounted under `/api`:

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/api/organizations` | List organizations (paged) |
| `GET` | `/api/organizations/{id}` | Find one by id |
| `GET` | `/api/organizations/path/{path...}` | Find one by hierarchical path |
| `POST` | `/api/organizations` | Create an organization |
| `PATCH` | `/api/organizations/{id}` | Edit an organization |
| `POST` | `/api/organizations/{id}/transfer` | Move it under a new parent |
| `DELETE` | `/api/organizations/{id}` | Delete an organization |

```sh
curl localhost:8080/api/organizations              # the seeded reference data, paged
curl localhost:8080/api/organizations/path/acme/engineering    # lookup by hierarchical path
```

## Tasks

Each task wraps a plain command, so the repository works without mise — with one caveat:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging instead of the local overlay's loopback
and debug. Set `APP_ENV=local` yourself when running without mise.

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

The compose file honors `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_DB`, and
`POSTGRES_PASSWORD` for the container, but the service does not read them: moving the container
off the defaults splits the two until the matching `APP_DATABASE_*` variable (or secrets entry)
follows. The defaults pair with `config.json` and `secrets.example.json`; change both sides
together.

## License

Apache 2.0 — see [LICENSE](LICENSE).
