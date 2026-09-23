# go-web-service

The reference web service of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), a
minimal-dependency Go implementation of
[Elemental Architecture](https://github.com/standards-lab/architecture/blob/main/architecture.md).
Initialized from the [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## Stack

The service is one composition on one declared stack, with one provider per capability. A variant
on another engine or identity provider would be a separate repository, never a switch inside
this one.

- SQL: Postgres 18, through [go-database](https://github.com/standards-lab/go-database) for the
  pool and its administration and [sqlate](https://github.com/standards-lab/sqlate) for the
  authored SQL. Locally it runs from the compose file. A managed deployment is planned on
  Azure Database for PostgreSQL or Amazon RDS through the same provider, changing only the
  configuration.
- Observability: OpenTelemetry, reached through a `docker compose` profile (`mise run otel-up`)
  that runs the collector and a local Loki, Tempo, Mimir, and Grafana stack — see
  [`compose/README.md`](compose/README.md). The service exports traces and metrics over OTLP and
  structured JSON logs correlated to them by trace id, all through
  [go-observability](https://github.com/standards-lab/go-observability).

### Standard and native tiers

Each use of a capability runs at one of two tiers, chosen per use. The standard tier uses the
technology's common standard: ISO/IEC 9075 SQL in an authored `.sql` file that declares
`--| tier: standard`, and RFC 9110 and RFC 9457 through go-web-sdk's `web` package. Domain
statements, handlers, and the composition root use the standard tier by default. The native
tier uses the provider's own features, in a `.sql` file that declares `--| tier: native` and
names the feature and how another engine expresses it. Native use is first-class, because the
point of choosing Postgres is to use it, and it is contained: each file declares its own tier,
`sqlint` fails a standard file that uses a native form, and the admin mount reports each
statement's tier.

Only the composition root, `internal/app/infrastructure.go`, imports a provider: it builds the
pool through go-database's provider and takes the dialect from sqlate's. Every other package is
provider-free, and native use stays in SQL files. The boundary is a convention; no linter
enforces it.

### What a provider swap changes

Moving to another SQL engine is a port, because the service owns a schema written for
Postgres. A port changes the two provider imports and the files below, and never the
configuration or the code around the statements:

- `data/migrations/0001_organization`, engine DDL by nature: `uuidv7()` as the id default (a
  Postgres 18 builtin; elsewhere the application or the engine mints ids), `UNIQUE NULLS NOT
  DISTINCT` for the sibling-scoped code (a partial unique index or a coalesced key elsewhere),
  and the `~` regular-expression `CHECK` on `code`.
- `data/patterns/identity.sql`: `RETURNING` for the identity every create returns (`OUTPUT
  INSERTED`, `RETURNING INTO`, or an insert and a read elsewhere).
- `data/statements/lock.sql`: `pg_advisory_xact_lock` over `hashtext` (the engine's application
  lock, or a `FOR UPDATE` mutex row, elsewhere).
- `data/statements/seed_organization.sql`: `ON CONFLICT ON CONSTRAINT ... DO NOTHING` with
  `RETURNING` for the idempotent seed (`MERGE` or `INSERT IGNORE` elsewhere).
- `domain/organization/statements/create.sql`: native through the identity pattern only.

The read path, the lineage CTE, and the guarded commands use the standard tier. This command
lists the native files:

```sh
grep -rl --include='*.sql' -- '--| tier: native' .
```

HTTP has no provider, and OpenTelemetry adds nothing to the list: the service uses only the
OpenTelemetry API, SDK, and OTLP, so a backend swap changes only the collector's configuration.

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks. The database password
lives in the gitignored secrets layer; copy the committed example to create it. A missing
`secrets.json` is not a load error: the service starts with an empty password and fails only at
the database connection, so this step comes first:

```sh
mise trust && mise install
cp secrets.example.json secrets.json

mise run db-up      # start the local Postgres and wait for health
mise run otel-up    # start the observability collector and stack
mise run serve      # run the service
```

`serve` streams its stdout to the collector over TCP and exits immediately if `otel-up` has not
run first — see [`compose/README.md`](compose/README.md) for the mechanism and its one accepted
limitation.

Telemetry starts first, ahead of every lifecycle stage: a startup hook installs the tracer and
meter providers before the pool connects, and a shutdown hook flushes them after the last stage
drains, so it brackets the numbered stages rather than holding one of its own. Startup then does
the database work itself, in lifecycle stages: the pool connects, the schema is verified and any
pending migration applied, the configured seed set is applied (the `local` overlay names
`default`, the reference tree), and each domain verifies its statements against the migrated
schema. The service then logs `server ready` on `localhost:8080` (the `local` overlay binds
loopback and runs debug logging). From a second shell:

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

Each task wraps a plain command, so the repository works without mise, with two caveats:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging, with no seed set, instead of the local
overlay's loopback, debug, and `default` set — set `APP_ENV=local` yourself when running without
mise. `serve`'s full command also needs a shell that understands `/dev/tcp` (bash, not `sh`); see
[`compose/README.md`](compose/README.md).

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet -tags integration ./...` | Compile-check and vet, the integration suite included |
| `mise run serve` | `go run ./cmd/server`, tee'd to the observability collector | Run the service locally |
| `mise run test` | `go test -race ./...` | Run the unit tier |
| `mise run integration` | an isolated `docker compose up`, `go test -tags integration ./integration/`, `down -v` | Run the integration tier against its own stack |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./... && go tool sqlint` | Lint the Go and the SQL |
| `mise run db-up` | `docker compose up -d --wait` | Start the local Postgres |
| `mise run db-down` | `docker compose down` | Stop the local Postgres (keep data) |
| `mise run db-reset` | `docker compose down -v` | Stop the local Postgres and drop its data |
| `mise run otel-up` | `docker compose ... up -d --wait`, then polls Mimir until it can query | Start the collector, Loki, Tempo, Mimir, and Grafana |
| `mise run otel-down` | `docker compose --profile observability down …` | Stop the observability profile (keep data) |
| `mise run otel-reset` | `docker compose --profile observability down -v …` | Stop the observability profile and drop its data |
| `mise run db-state <state>` | `curl -d '{"state":"<state>"}' localhost:8080/admin/database/state` | Reset the running service's database to a named state |
| `mise run slab -- list` | `cd tools/slab && go run ./cmd/slab` | Run the narrated demo scenarios — see [`tools/slab/README.md`](tools/slab/README.md) |

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
   `APP_OBSERVABILITY_ENDPOINT` and `APP_OBSERVABILITY_SAMPLE_RATIO`, the rate limit
   (`APP_RATE_LIMIT_REQUESTS`, `APP_RATE_LIMIT_WINDOW`), the reads paging policy
   (`APP_READS_DEFAULT_SIZE`, `APP_READS_MAX_SIZE`), and the admin seed set (`APP_ADMIN_SEED`, a
   state name).

Every file is optional: a deployment can run on the base file and environment variables alone,
or on environment variables only.

The compose file honors `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_DB`, and
`POSTGRES_PASSWORD` for the container, but the service does not read them: moving the container
off the defaults splits the two until the matching `APP_DATABASE_*` variable (or secrets entry)
follows. The defaults pair with `config.json` and `secrets.example.json`; change both sides
together.

## License

Apache 2.0, see [LICENSE](LICENSE).
