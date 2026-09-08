# go-web-service

The reference web service of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), a
minimal-dependency Go implementation of
[Elemental Architecture](https://github.com/standards-lab/architecture/blob/main/architecture.md).
Initialized from the [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## Stack

- SQL: Postgres, through [go-database](https://github.com/standards-lab/go-database) for the pool
  and its administration and [sqlate](https://github.com/standards-lab/sqlate) for the authored
  SQL.

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks. The database password
lives in the gitignored secrets layer; copy the committed example to create it. A missing
`secrets.json` is not a load error: the service starts with an empty password and fails only at
the database connection, so this step comes first:

```sh
mise trust && mise install
cp secrets.example.json secrets.json

mise run db-up      # start the local Postgres and wait for health
mise run serve      # run the service
```

Startup does the database work itself, in lifecycle stages: the pool connects, the schema is
verified and any pending migration applied, the configured seed set is applied (the `local`
overlay names `default`, the reference tree), and each domain verifies its statements against
the migrated schema. The service then logs `server ready` on `localhost:8080` (the `local` overlay
binds loopback and runs debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}  — lifecycle, database, schema
```

Ctrl-C drains in-flight requests and exits with `server stopped`. That serve, probe, and
drain sequence is the one check a change to the composition root is verified by hand with;
everything the running service does is asserted by the integration tier below.

## API

The organization domain is mounted under `/api`:

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/api/organizations` | List organizations (paged, filtered, sorted) |
| `GET` | `/api/organizations/{id}` | Find one by id |
| `GET` | `/api/organizations/path/{path...}` | Find one by hierarchical path |
| `POST` | `/api/organizations` | Create an organization |
| `PUT` | `/api/organizations/{id}` | Replace its code and name |
| `POST` | `/api/organizations/{id}/transfer` | Move it under a new parent (`parent_id`, null for the root) |
| `DELETE` | `/api/organizations/{id}` | Delete an organization |

The list takes `page`, `size`, and `sort` (`sort=-path,code`), and every other parameter as a
filter on a field of the read model: `code=acme` is equality, `code=acme&code=finance` is
membership, and `name[like]=%25ing` names an operator in brackets. The guarded commands (`PUT`,
transfer, `DELETE`) take the row's version in `If-Match: "3"`; a missing header answers 428 and a
stale version 412. Every rejection is an RFC 9457 problem.

```sh
curl localhost:8080/api/organizations                          # the seeded reference data, paged
curl localhost:8080/api/organizations/path/acme/engineering    # lookup by hierarchical path
```

## Admin

The database admin service is mounted under `/admin/database`. Every endpoint triggers the same
library function startup runs; the schema verbs answer with the resulting schema status.

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/database/diagnostics` | Ping latency, server version, pool counters |
| `GET` | `/admin/database/schema` | The migration history against the embedded set |
| `GET` | `/admin/database/patterns` | The pattern catalog |
| `GET` | `/admin/database/statements` | Every domain's compiled statements |
| `GET` | `/admin/database/states` | The named states the service declares |
| `POST` | `/admin/database/schema/verify` | Verify the history and the seed statements |
| `POST` | `/admin/database/schema/up` | Apply every pending migration |
| `POST` | `/admin/database/schema/down` | Revert migrations (`{"steps": 1}`, the default) |
| `POST` | `/admin/database/schema/steps` | Apply or revert `{"steps": n}`, negative to revert |
| `POST` | `/admin/database/schema/force` | Set the history to `{"version": v}` without running a file |
| `POST` | `/admin/database/seed` | Apply the configured set, or `{"state": "…"}`, over what is there (403 with no set) |
| `POST` | `/admin/database/state` | Reset to `{"state": "…"}`: revert every migration, apply the set, seed the state |

A named state is one file under `data/seeds/`, keyed by table: `default` is the reference
tree, `empty` is the schema with no rows. A set applies idempotently, so `admin.seed` names
the state a deployment initializes with at its first start and leaves alone at every later
one. The state operation is destructive, in the class of `down` and `force`, and
`mise run db-state <state>` runs it against the local service.

The mount serves on the API listener until the management listener lands; it is not for a
public deployment as it stands.

## Tasks

Each task wraps a plain command, so the repository works without mise, with one caveat:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging, with no seed set, instead of the local
overlay's loopback, debug, and `default` set. Set `APP_ENV=local` yourself when running without
mise.

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet -tags integration ./...` | Compile-check and vet, the integration suite included |
| `mise run serve` | `go run ./cmd/server` | Run the service locally |
| `mise run test` | `go test -race ./...` | Run the unit tier |
| `mise run integration` | an isolated `docker compose up`, `go test -tags integration ./integration/`, `down -v` | Run the integration tier against its own stack |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./... && go tool sqlint` | Lint the Go and the SQL |
| `mise run db-up` | `docker compose up -d --wait` | Start the local Postgres |
| `mise run db-down` | `docker compose down` | Stop the local Postgres (keep data) |
| `mise run db-reset` | `docker compose down -v` | Stop the local Postgres and drop its data |
| `mise run db-state <state>` | `curl -d '{"state":"<state>"}' localhost:8080/admin/database/state` | Reset the running service's database to a named state |

## Tests

Two tiers. The unit tier, `mise run test`, runs on every pull request and touches no service,
network, or disk: a package that runs SQL proves it over sqlate's scripted driver. The
integration tier, `mise run integration`, runs the composed service black-box through its API
against the compose stack, in CI on every merge to main and on demand from the Actions tab.

The suite lives in the `integration` package under the `integration` build tag. Its harness is
the toolkit the SDKs ship beside what it exercises: go-core's `process/processtest` builds
`cmd/server` once, runs it as a subprocess configured by `APP_*` variables on a reserved port,
and relays the database through a loopback forwarder the outage test severs; go-web-sdk's
`webtest` drives it through its API. The service adds only its configuration and state control
through the admin mount; nothing in the service exists for the tests' sake. The task runs
the same `compose.yml` as its own project (`go-web-service-integration`, Postgres on 5433), so
the development stack and its data are never touched, and tears the stack down with its volume
when the suite ends, so every run starts from an empty database. The harness honors
`APP_DATABASE_HOST`, `APP_DATABASE_PORT`, and `APP_DATABASE_PASSWORD` for a stack elsewhere.

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json`, the base file, every knob at its default.
2. `config.<APP_ENV>.json`, the environment overlay; `mise.toml` sets `APP_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging, the `default`
   seed set).
3. `secrets.json` and `secrets.<APP_ENV>.json`, gitignored secret layers; the database password
   goes here.
4. `APP_*` environment variables, the final override: the log, server, and shutdown variables,
   the `APP_DATABASE_*` family (`APP_DATABASE_HOST`, `APP_DATABASE_NAME`,
   `APP_DATABASE_USER`, `APP_DATABASE_PASSWORD`, `APP_DATABASE_PORT`, and the pool settings),
   the reads paging policy (`APP_READS_DEFAULT_SIZE`, `APP_READS_MAX_SIZE`), and the admin
   seed set (`APP_ADMIN_SEED`, a state name).

Every file is optional: a deployment can run on the base file and environment variables alone,
or on environment variables only.

The compose file honors `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_DB`, and
`POSTGRES_PASSWORD` for the container, but the service does not read them: moving the container
off the defaults splits the two until the matching `APP_DATABASE_*` variable (or secrets entry)
follows. The defaults pair with `config.json` and `secrets.example.json`; change both sides
together.

## License

Apache 2.0, see [LICENSE](LICENSE).
