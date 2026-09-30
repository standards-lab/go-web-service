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
- Object storage: Azure Blob Storage, through
  [go-storage](https://github.com/standards-lab/go-storage) and its `azureblob` provider, with
  [blobfs](https://github.com/standards-lab/blobfs) and its `postgres` engine keeping the file
  tree's rows in the same Postgres. Locally it runs Azurite from the compose file.
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
pool through go-database's provider, takes the dialect from sqlate's, builds the object store's
client through go-storage's `azureblob`, and installs blobfs's `postgres` engine with its
migration set. Every other package is provider-free, and native use stays in SQL files. The one
exception is the integration harness, which builds its own `azureblob` client to read the store
beneath the API. The boundary is a convention; no linter enforces it.

### What a provider swap changes

Each technology is classed by what moving between its providers costs.

SQL is schema-bound: moving to another SQL engine is a port, because the service owns a schema
written for Postgres. A port changes the SQL provider imports (go-database's, sqlate's, and
blobfs's engine with its migration set) and the files below, and never the configuration or the
code around the statements:

- `data/migrations/0001_organization`, engine DDL by nature: `uuidv7()` as the id default (a
  Postgres 18 builtin; elsewhere the application or the engine mints ids), `UNIQUE NULLS NOT
  DISTINCT` for the sibling-scoped code (a partial unique index or a coalesced key elsewhere),
  and the `~` regular-expression `CHECK` on `code`.
- `data/migrations/0002_organization_image`: `uuidv7()` as the id default, and the partial
  unique index that admits one active logo per organization (a filtered index, or a guard in
  the activation, elsewhere).
- `data/patterns/identity.sql`: `RETURNING` for the identity every create returns (`OUTPUT
  INSERTED`, `RETURNING INTO`, or an insert and a read elsewhere).
- `data/statements/lock.sql`: `pg_advisory_xact_lock` over `hashtext` (the engine's application
  lock, or a `FOR UPDATE` mutex row, elsewhere).
- `domain/organization/statements/seed.sql`: `ON CONFLICT ON CONSTRAINT ... DO NOTHING` with
  `RETURNING` for the idempotent seed (`MERGE` or `INSERT IGNORE` elsewhere).
- `domain/organization/statements/create.sql`: native through the identity pattern only.

The read path, the lineage CTE, the guarded commands, the logo's and the document root's
statements, and the document seed's path walk
(`domain/document/statements/seed_organization.sql`) use the standard tier. This command lists
the native files:

```sh
grep -rl --include='*.sql' -- '--| tier: native' .
```

Object storage is interchangeable with review. The service reaches the store only through
go-storage's standard operations (put, get, delete, the container's ensure and probe, and the
provider's key rule), so moving to another object store, S3 for one, changes the provider
import in the composition root and the `storage` configuration. The move also needs a review of
what the common API leaves unstated: the store's consistency, and the ETag it mints, which the
download and the logo serve as their validator. blobfs's rows move with the SQL engine, not the
store.

HTTP has no provider, and OpenTelemetry adds nothing to the list: the service uses only the
OpenTelemetry API, SDK, and OTLP, so a backend swap changes only the collector's configuration.

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks. The database password
and the object store's key live in the gitignored secrets layer; copy the committed example to
create it. A missing `secrets.json` is not a load error: the configuration loads without them,
and the service then exits at construction with `storage: azureblob: storage key required`, so
this step comes first:

```sh
mise trust && mise install
cp secrets.example.json secrets.json

mise run db-up      # start the local Postgres and Azurite and wait for health
mise run otel-up    # start the observability collector and stack
mise run serve      # run the service
```

`serve` streams its stdout to the collector over TCP and exits immediately if `otel-up` has not
run first — see [`compose/README.md`](compose/README.md) for the mechanism and its one accepted
limitation.

Startup connects the database and the object store, applies any pending migration, checks every
statement against the schema, seeds the configured state (the `local` overlay names `default`),
starts the [sweep](#sweep), and starts the server last. The drain runs in reverse. The stage
table in `internal/app/stages.go` is the one declaration of that order. The service then logs
`server ready` on `localhost:8080` (the `local` overlay binds loopback and runs debug logging).
From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}  — lifecycle, database, storage, schema, sweeper
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
| `GET` | `/api/organizations/lookup?path=/acme/engineering` | Find one by hierarchical path |
| `POST` | `/api/organizations` | Create an organization |
| `PUT` | `/api/organizations/{id}` | Replace its code and name |
| `POST` | `/api/organizations/{id}/transfer` | Move it under a new parent (`parent_id`, null for the root) |
| `DELETE` | `/api/organizations/{id}` | Delete an organization |
| `PUT` | `/api/organizations/{id}/logo` | Store its logo (raw body), replacing any |
| `GET` | `/api/organizations/{id}/logo` | Read its logo's bytes |
| `DELETE` | `/api/organizations/{id}/logo` | Delete its logo |

The list takes `page`, `size`, and `sort` (`sort=-path,code`), and every other parameter as a
filter on a field of the read model: `code=acme` is equality, `code=acme&code=finance` is
membership, and `name[like]=%25ing` names an operator in brackets. Its envelope carries the
filtered `total`, `more`, and a `next` cursor. Sending `cursor=<next>` in place of `page`, with
the same sort and filters, continues after the last row, so a tree that changes between
requests neither skips nor repeats one. A continued page omits `page`; a sort on `parent_id`,
the one nullable field, pages by number alone. The total is counted in the page's own
statement, from the rows it returns, so a page past the last, which returns none, omits `total`
too; its `more` is false. The document listings follow the same contract.

The guarded commands (`PUT`, transfer, `DELETE`) take the row's version in `If-Match: "3"`; a
missing header answers 428 and a stale version 412. Every rejection is an RFC 9457 problem. A
conflict, 409, carries a curated `detail` naming its kind and never the underlying error's
text: "the request conflicts with the current state" for a taken code, a missing parent, a
cycle, a concurrent logo replacement, and a delete of an organization that still has children,
a logo, or a document root, and "the file is being deleted" for a logo `PUT` whose new file's
delete began before its activation. A seeded organization holds a logo, so its delete answers
409 until its logo is deleted.

The logo is a raw body of at most 1 MiB in a raster type, PNG, JPEG, WebP, or GIF; any other
type, SVG included, answers 415. A `PUT` answers 201 with the logo's `Location` and
`{"id": …}`, the new file's id, and carries no version. It answers 201 whether or not it
replaced a logo, since every `PUT` stores a new file. The logo routes take no `If-Match`: the
last write wins, and a 409, not a precondition, tells apart two replacements racing to
activate. The `GET` is served `Cache-Control: no-cache` with the object's `ETag` and
`Last-Modified`, so a client revalidates on every use. Once a replacement or a delete commits,
the service purges the old file's object and row. If the purge fails, the service logs it at
warn and the request still succeeds (201 or 204); the file stays deleting until the sweep's stale
reclaim.

```sh
curl localhost:8080/api/organizations                          # the seeded reference data, paged
curl 'localhost:8080/api/organizations/lookup?path=/acme/engineering'  # lookup by path
```

The document domain is mounted under `/api/documents/{org}`, each organization's hierarchy of
directories and files over blobfs and the object store. A directory id may be `root`, the
organization's document root, which its first write creates. Until then, `root`'s listings
answer an empty page, or 404 for an organization that does not exist; like any listing, they
refuse a bad sort, filter, or cursor with 400:

| Method | Path | What it does |
|--------|------|--------------|
| `POST` | `/directories` | Create a directory (`parent_id`, `name`) |
| `GET` | `/directories/{id}` | Read a directory with its path and `status` (`active` or `deleting`) |
| `GET` | `/directories/{id}/directories` | List its child directories (paged, filtered, sorted) |
| `GET` | `/directories/{id}/files` | List its files, pending and available (paged, filtered, sorted) |
| `DELETE` | `/directories/{id}` | Delete an empty directory (204), or with `?recursive=true` its branch (202) |
| `POST` | `/directories/{id}/move` | Move it under a new parent within the root |
| `POST` | `/directories/{id}/files?name={name}` | Upload a file (raw body, at most 10 MiB) |
| `GET` | `/files/{id}` | Read a file's metadata |
| `GET` | `/files/{id}/content` | Download an available file, as an attachment |
| `DELETE` | `/files/{id}` | Delete a file |
| `POST` | `/files/{id}/move` | Move it into a directory within the root |

- An upload is a `POST` to its directory's files, with the file's name in the required `name`
  parameter. It answers 201 with the file's metadata read as its `Location`. It creates a new
  file under an id the server mints, so it is not idempotent: a retry after a lost response
  answers 409 on the name the first took.
- An upload the store refuses while it is unreachable answers 503. If the store also refuses
  the abandon's object delete, the file is left deleting, hidden from the listings, and keeps
  its name until the sweep's stale reclaim (`sweep.stale_age`); an upload of that name
  meanwhile answers 409 "the file is being deleted". An upload whose body ends before its
  declared length answers 400, since the request is at fault, never 503.
- The moves and the deletes take the row's version in `If-Match`: 428 when it is missing, 412
  when it is stale.
- A recursive delete marks the branch deleting and answers 202 with the directory's read as its
  `Location`; the [sweep](#sweep) then removes the rows and their objects. Until it finishes,
  the directory reads `deleting`, its listings answer 404, and a write into it answers 409 "the
  directory is being deleted". A repeated recursive delete answers 202 and nudges the sweep
  again. A recursive delete of `root` removes the document root, and the next write creates a
  new one.
- Every conflict carries one of six curated details: "an entry with that name already exists",
  "the directory is not empty", "the directory is being deleted", "the file is being deleted",
  "the file is referenced", or "the request conflicts with the current state". A move of a file
  whose own delete began, in a directory that is not deleting, answers "the file is being
  deleted"; a file in a marked branch, or a move into one, answers the directory's detail.

An id outside the organization's document root answers 404, as an absent one does, so no
request reads, moves, or deletes across organizations. A download is served as
`Content-Disposition: attachment` with `Cache-Control: private, no-cache`, never rendered
inline, since stored HTML would run in the API's origin. A download whose object cannot be
opened answers its problem with `Cache-Control: no-store` and none of the file's headers.

Two validators share HTTP's entity-tag syntax, the version and the object ETag, and neither
stands in for the other:

- The version is a row's body field in every read and command response, never an `ETag`
  header. A client quotes the version it read as the `If-Match` of a guarded command (`"3"`).
- The object ETag is the object store's tag for a download's or a logo's bytes, served as
  `ETag` with `Last-Modified` beside it; a conditional `GET` naming either (`If-None-Match`,
  `If-Modified-Since`) answers 304. The object ETag is not a version, and `If-Match` refuses a
  download's as malformed.

## Admin

The database admin service is mounted under `/admin/database`. Every endpoint triggers the same
library function startup runs; the schema verbs answer with the resulting schema status.

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/database/diagnostics` | Ping latency, server version, pool counters |
| `GET` | `/admin/database/schema` | Each migration set's history against its embedded migrations |
| `GET` | `/admin/database/patterns` | The pattern catalog |
| `GET` | `/admin/database/statements` | Every domain's compiled statements |
| `GET` | `/admin/database/states` | The named states the service declares |
| `POST` | `/admin/database/schema/verify` | Verify every set's history and every statement |
| `POST` | `/admin/database/schema/up` | Apply every set's pending migrations |
| `POST` | `/admin/database/schema/down` | Revert a set's migrations (`{"set": "app", "steps": 1}`, one by default) |
| `POST` | `/admin/database/schema/steps` | Apply or revert a set's `{"set": "app", "steps": n}`, negative to revert |
| `POST` | `/admin/database/schema/force` | Set a set's history to `{"set": "app", "version": v}` without running a file |
| `POST` | `/admin/database/seed` | Apply the configured set, or `{"state": "…"}`, over what is there (403 with no set) |
| `POST` | `/admin/database/state` | Reset to `{"state": "…", "confirm": true}`: revert every set, apply them, seed the state (the configured set when `state` is empty; 403 with none) |

The schema is two migration sets, run in declaration order: `blobfs`, blobfs's own tables, and
`app`, the service's (`data/migrations`), whose migrations reference the tables beneath. The
status reports each set; `up` and `verify` act on every set, and `down`, `steps`, and `force`
name the set they act on, refused with 400 when it is missing or undeclared.

A named state is one file under `data/seeds/`, keyed by each domain's seed contribution:
`organizations`, `logos`, and `documents`. `default` is the reference tree, a logo for each
organization, and acme's document tree; `empty` is nothing. The files a state names sit under
`data/seeds/fixtures/`. `admin.seed` names the state the service seeds at every start, not only
the first; of the checked-in configurations, only the local overlay names one (`default`). A
seed applies idempotently: it leaves a row or file that exists as it is and writes again any the
state names that is missing, so a seeded organization, logo, or file deleted since the last
start is restored at the next. The seed's response, and the state's `seeded` member, count what
the run stored under the same keys. The rows commit in one transaction first, and the files are
written after it commits, since a file's object is put outside any transaction.

The state operation is destructive, in the class of `down` and `force`, and
`mise run db-state <state>` runs it against the local service. A reset reverts blobfs's tables
but leaves the objects in the container, which is accepted in development: a seeded file carries
a fixed id (the `5eed…` ids in the state file), so the reseed writes it again under the same key
and replaces the object left there.

The verbs that change the schema (`up`, `down`, `steps`, and the state reset) share a quiesce
gate with the sweep: a verb waits for a sweep pass in flight to finish, and the sweep starts no
pass until the verb is done. Without the gate, a pass and a reset would deadlock on the
document root's owner table. The gate is per process: with several replicas, another replica's
sweep can still meet a reset, which is a development operation.

The object storage admin service is mounted under `/admin/storage`:

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/storage/diagnostics` | Whether a live probe succeeds, the configured container, the provider's longest key |
| `POST` | `/admin/storage/container` | Create the configured container if it is missing, then answer with the diagnostics |

A store that cannot be reached answers 503 with the provider's reason. The admin mount serves on
the API listener until the management listener lands; it is not for a public deployment as it
stands.

A container deleted while the service runs makes every storage operation a 503, a download's
read included, and takes the storage check on `/readyz` down; the rows are untouched. `POST
/admin/storage/container` creates it again, empty, and readiness recovers, but each stored
file's row now names an object that no longer exists, and its download answers 404. In
development, a reset to a seeded state (`mise run db-state default`) writes the seeded files
again; anything else is uploaded again. The service does not reconcile rows against the store:
blobfs defers that reconciler to the event flow `v1.messaging` brings.

## Sweep

The sweep finishes the rows that a recursive directory delete, a write that stops partway, and a
delete that stops partway leave for later: a branch marked deleting, a pending upload, a file
whose delete began. It removes each file's object and then its row, and each directory once it
is empty. The sweep is the `data` package's background worker (`data.Storage.SweepWorker`),
which runs blobfs's sweep in bounded passes while a pass reports more. Once the service begins to
drain, the sweep finishes the pass in flight and runs no further one, whatever remains, so a
large backlog never holds the drain; the next start's wake finds what is left.

A Reactor, in the architecture's sense, calls a Domain Service. The sweep calls none, so it is
not one; it is the exception the composition root stages as a reactor anyway, since the reactor
is the process's one runner for work that lasts the process lifetime.

It wakes on each recursive delete, on an interval (`sweep.interval`, 30 seconds) for work no
delete announced, and once at startup, so a branch marked before a restart is swept at the
next start. A pass reclaims uploads and deletes left unfinished past `sweep.stale_age` (an
hour), and handles at most `sweep.batch` (100) records. A pass the store or the database
refuses is logged at warn and retried on the next wake; it never fails the service. `/readyz`
reports the reactor as the `sweeper` check.

## Tasks

Each task wraps a plain command, so the repository works without mise, with two caveats:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging, with no seed set, instead of the local
overlay's loopback, debug, and `default` set — set `APP_ENV=local` yourself when running without
mise. `serve`'s full command also needs a shell that understands `/dev/tcp` (bash, not `sh`); see
[`compose/README.md`](compose/README.md).

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet -tags integration ./...`, then `go vet ./...` in `tools/slab` | Compile-check and vet, the integration suite and slab included |
| `mise run serve` | `go run ./cmd/server`, tee'd to the observability collector | Run the service locally |
| `mise run test` | `go test -race ./...`, then the same in `tools/slab` | Run the unit tier, slab included |
| `mise run integration` | an isolated `docker compose up`, `go test -tags integration ./integration/`, `down -v` | Run the integration tier against its own stack |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./... && go tool sqlint`, then `golangci-lint run ./...` in `tools/slab` | Lint the Go and the SQL, slab included |
| `mise run db-up` | `docker compose up -d --wait` | Start the local Postgres and Azurite |
| `mise run db-down` | `docker compose down` | Stop the local Postgres and Azurite (keep data) |
| `mise run db-reset` | `docker compose down -v` | Stop the local Postgres and Azurite and drop their data |
| `mise run otel-up` | `docker compose ... up -d --wait`, then polls Mimir until it can query | Start the collector, Loki, Tempo, Mimir, and Grafana |
| `mise run otel-down` | `docker compose --profile observability down …` | Stop the observability profile (keep data) |
| `mise run otel-reset` | `docker compose --profile observability down -v …` | Stop the observability profile and drop its data |
| `mise run db-state <state>` | `curl -d '{"state":"<state>","confirm":true}' localhost:8080/admin/database/state` | Reset the running service's database to a named state |
| `mise run slab -- list` | `cd tools/slab && go run ./cmd/slab` | Run the narrated demo scenarios — see [`tools/slab/README.md`](tools/slab/README.md) |

`mise run slab -- demo storage` narrates the storage end to end against the running service: the
two migration sets, the seeded logos and document tree, the validators and refusals, and a
recursive delete waited out through the sweep.

## Tests

Two tiers. The unit tier, `mise run test`, runs on every pull request and touches no service,
network, or disk: a package that runs SQL proves it over sqlate's scripted driver, and one that
stores objects over go-storage's fake. The integration tier, `mise run integration`, runs the
composed service black-box through its API against the compose stack, in CI on every merge to
main and on demand from the Actions tab.

The suite lives in the `integration` package under the `integration` build tag. Each file is
named for the surface it asserts (`organization_test.go`, `document_test.go`, `logo_test.go`,
`sweep_test.go`, `outage_test.go`, and the rest), and each test's comment states what it proves.
Its harness is the toolkit the SDKs ship beside what it exercises: go-core's
`process/processtest` builds `cmd/server` once, runs it as a subprocess configured by `APP_*`
variables on a reserved port, and relays the database or the object store through a loopback
forwarder (`Options.Database`, `Options.Storage`) that a test severs for an outage. go-web-sdk's
`webtest` drives the service through its API, and `integration.Objects` reads the container
beneath it. A failing test prints each service process's output, so a 500 comes with the
service's own record of it. The service adds only its configuration and state control through
the admin mount; nothing in the service exists for the tests' sake.

The task runs the same `compose.yml` as its own project (`go-web-service-integration`, Postgres
on 5433 and Azurite on 10001), so the development stack and its data are never touched, and
tears the stack down with its volumes when the suite ends, so every run starts from an empty
database and container. The harness honors `APP_DATABASE_HOST`, `APP_DATABASE_PORT`,
`APP_DATABASE_PASSWORD`, `APP_STORAGE_ENDPOINT`, `APP_STORAGE_ACCOUNT`, and `APP_STORAGE_KEY` for
a stack elsewhere.

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json`, the base file, every knob at its default.
2. `config.<APP_ENV>.json`, the environment overlay; `mise.toml` sets `APP_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging, the `default`
   seed set).
3. `secrets.json` and `secrets.<APP_ENV>.json`, gitignored secret layers; the database password
   and the object store's key go here.
4. `APP_*` environment variables, the final override: the log, server, and shutdown variables,
   the `APP_DATABASE_*` family (`APP_DATABASE_HOST`, `APP_DATABASE_NAME`,
   `APP_DATABASE_USER`, `APP_DATABASE_PASSWORD`, `APP_DATABASE_PORT`, and the pool settings),
   the `APP_STORAGE_*` family (below), `APP_OBSERVABILITY_ENDPOINT` and
   `APP_OBSERVABILITY_SAMPLE_RATIO`, the rate limit (`APP_RATE_LIMIT_REQUESTS`,
   `APP_RATE_LIMIT_WINDOW`), the reads paging policy
   (`APP_READS_DEFAULT_SIZE`, `APP_READS_MAX_SIZE`), the admin seed set (`APP_ADMIN_SEED`, a
   state name), and the sweep's interval, pass size, and stale age (`APP_SWEEP_INTERVAL`,
   `APP_SWEEP_BATCH`, `APP_SWEEP_STALE_AGE`). An empty value counts as unset, as it does for every
   override, so only a file's `"seed": ""` clears a configured seed set.

Every file is optional: a deployment can run on the base file and environment variables alone,
or on environment variables only. Each file decodes strictly: a key the configuration does not
declare fails the load.

The `storage` block is go-storage's, each setting with its override:

| Setting | Variable | Default |
|---------|----------|---------|
| `endpoint` | `APP_STORAGE_ENDPOINT` | none, the account's public service URL; the `local` overlay names Azurite's `http://127.0.0.1:10000/devstoreaccount1` |
| `container` | `APP_STORAGE_CONTAINER` | `go-web-service` (`config.json`), required |
| `account` | `APP_STORAGE_ACCOUNT` | none, required; the `local` overlay names `devstoreaccount1` |
| `key` | `APP_STORAGE_KEY` | none, required; a secret, and `secrets.example.json` carries Azurite's published development key |
| `max_object_size` | `APP_STORAGE_MAX_OBJECT_SIZE` | 10485760 bytes, 10 MiB (`config.json`); 0 is unbounded |
| `list_page_size` | `APP_STORAGE_LIST_PAGE_SIZE` | 0, the provider's own page size |
| `request_timeout` | `APP_STORAGE_REQUEST_TIMEOUT` | `3s` (`config.json`; go-storage's own is `10s`), bounding the store's own startup check and readiness probe, so `/readyz` answers within it when the store is down; the object operations take no bound from it |
| `read_idle_timeout` | `APP_STORAGE_READ_IDLE_TIMEOUT` | `30s`, bounding each read of a download's body from the store while the read is in progress, so a store that stops sending is cut off and a slow client never is |
| `options.<key>` | `APP_STORAGE_OPTIONS_<KEY>` | azureblob's own settings: `try_timeout`, `max_retries`, `block_size`, and `concurrency`, described below |

azureblob's `try_timeout` bounds each try of one object operation, never a whole transfer: a
`Put` block's upload, or the part of a `Get`'s body read within the try. A download's body
resumes past a try's deadline, so it runs as long as its client reads. `config.json` sets it to
`10s`, below `read_idle_timeout`, so a stalled try resumes once before the store is cut off; a
stalled store holds an upload for at most one try per attempt. `max_retries` sets how many times
a failed try is retried, and how many times a download's body resumes per read. `config.json`
keeps the SDK's 3, with exponential backoff from 800 ms; the `local` overlay and the integration
harness set `1`, so a store that is down is refused in about a second. `block_size` and
`concurrency` size a `Put`'s staged blocks.

The server's `read_timeout` and `write_timeout` are `30s` (`config.json`, go-web-sdk's
defaults), sized for a request that moves no large body. An upload or a download sets its own
connection deadlines through go-web-sdk's `Transfer`: the timeout plus the time the body takes at
`transfer_rate` (`APP_SERVER_TRANSFER_RATE`, 65536 bytes per second), the slowest pace a client
is allowed. The body's size counts up to the route's limit (10 MiB for a document, 1 MiB for a
logo). An upload slower than that pace answers 408; a download slower than that ends short.

The `sweep` block schedules the [sweep](#sweep): `interval` (`APP_SWEEP_INTERVAL`, `30s`), the
backstop wake; `batch` (`APP_SWEEP_BATCH`, 100), the records one pass handles; and `stale_age`
(`APP_SWEEP_STALE_AGE`, `1h`), the age past which a pass reclaims an unfinished upload or delete.

The compose file honors `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_DB`, and
`POSTGRES_PASSWORD` for the Postgres container and `AZURITE_BLOB_PORT` for Azurite's, but the
service does not read them: moving a container off the defaults splits the two until the
matching `APP_DATABASE_*` or `APP_STORAGE_ENDPOINT` variable (or secrets entry) follows. The
defaults pair with `config.json` and `secrets.example.json`; change both sides together.

## License

Apache 2.0, see [LICENSE](LICENSE).
