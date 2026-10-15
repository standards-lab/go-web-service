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
- Messaging: NATS JetStream, through [go-messaging](https://github.com/standards-lab/go-messaging)
  and its `nats` provider, with its `postgres` engine keeping the outbox and inbox in the same
  Postgres. Locally it runs NATS from the compose file.
- Observability: OpenTelemetry, reached through a `docker compose` profile (`mise run otel:up`)
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

Only the composition root (`internal/app`) imports a provider. `infrastructure.go` builds the
pool through go-database's provider, takes the dialect from sqlate's, builds the object store's
client through go-storage's `azureblob` and the broker through go-messaging's `nats`, and
installs blobfs's `postgres` engine; `admin.go` declares blobfs's and go-messaging's `postgres`
migration sets, and `reactors.go` gives the messaging runtime go-messaging's `postgres` outbox
and inbox. The configuration, `internal/config`, imports go-messaging's `nats` too, for its
`nats` block alone: go-messaging keeps the broker's settings with its provider, not in its base,
as go-database and go-storage keep theirs. Every other package is provider-free, and native use
stays in SQL files. There are
two exceptions, both outside the service's process: the integration harness, which builds its
own clients (go-database's `postgres` pool, `azureblob`, and go-messaging's `nats` broker) to
reach the database, the store, and the stream beneath the API; and the tool module's
`tools/slab/events`, which imports go-messaging's `nats` and nats.go to watch the stream. The
boundary is a convention; no linter enforces it.

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

mise run db:up      # start the local Postgres, Azurite, and NATS and wait for health
mise run otel:up    # start the observability collector and stack
mise run serve      # run the service
```

`serve` streams its stdout to the collector over TCP and exits immediately if `otel:up` has not
run first — see [`compose/README.md`](compose/README.md) for the mechanism and its one accepted
limitation.

The service is one dependency graph, described in `internal/app`, and startup runs it in layer
order. Telemetry starts first, so its providers are installed before anything they record. The
database, the object store, and the NATS broker connect next, together in one layer. The schema
starts after them: it applies any pending migration, checks every statement against the schema,
and seeds the configured state (the `local` overlay names `default`). The [sweep](#sweep) and
the [messaging](#messaging) relay start after the schema, and the server starts last, alone in
the top layer. The drain runs in reverse: the server drains first, so in-flight requests
complete before what they run on closes; the relay then makes its last pass, publishing what
those requests committed, before the broker and the database close; and telemetry flushes
last. The service then logs `server ready` on `localhost:8080` (the `local` overlay binds
loopback and runs debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}  — lifecycle, database, storage, broker, schema, sweeper, relay
```

Ctrl-C drains in-flight requests and exits with `server stopped`. That serve, probe, and
drain sequence is the one check a change to the composition root is verified by hand with;
everything the running service does is asserted by the integration tier below.

### Adding a service

A service is a node in the graph. Define it in its layer's define function, in that layer's file
in `internal/app` (`defineDomain` in `domain.go` for a domain service, for example), and add its
handle to `Nodes`. Its part in the lifecycle is inferred from its value's methods: a `Start` or
`Shutdown` makes it a participant, a `Ready` on a value that also has `Start` adds its check to
`/readyz` (a `Ready` alone adds none), and an `Err` channel has its runtime failure watched, so
nothing registers it. Its place in the order follows from the nodes it uses. A reactor's node
also joins `Nodes.Reactors`, which `Run` builds as roots and the server orders itself after. A
node that commits events outside a request, such as a consuming reactor, Uses the relay's node,
so it drains before the relay's last pass; a request-driven producer, as every domain service
is, needs no edge, since the server drains first.

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
  declared length answers 400, and one whose body arrives slower than `server.transfer_rate`
  allows answers 408, since the request is at fault, never 503.
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

The schema is three migration sets, run in declaration order: `messaging`, go-messaging's outbox
and inbox; `blobfs`, blobfs's own tables; and `app`, the service's (`data/migrations`), whose
migrations reference the tables beneath. The
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
written after it commits, since a file's object is put outside any transaction. Each entity the
seed creates commits its command's event, and a rerun that finds everything there emits nothing
([Messaging](#the-event-convention)).

The state operation is destructive, in the class of `down` and `force`, and
`mise run db:state <state>` runs it against the local service. A reset reverts the messaging
set too, dropping any event still pending in the outbox, and commits one
`web-service.database.reset` before it seeds. It reverts blobfs's tables but leaves
the objects in the container, which is accepted in development: a seeded file carries
a fixed id (the `5eed…` ids in the state file), so the reseed writes it again under the same key
and replaces the object left there.

The verbs that change the schema (`up`, `down`, `steps`, and the state reset) share a quiesce
gate with the sweep and the relay: a verb waits for a sweep pass or a relayed row in flight to
finish, and neither starts another until the verb is done. Without the gate, a pass and a reset would deadlock on the
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
development, a reset to a seeded state (`mise run db:state default`) writes the seeded files
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

The sweep is a Reactor in the architecture's sense, one that dispatches to no Domain Service: the
composition root runs it on a reactor, the process's one runner for work driven by an
occurrence for the process lifetime.

It wakes on each recursive delete, on an interval (`sweep.interval`, 30 seconds) for work no
delete announced, and once at startup, so a branch marked before a restart is swept at the
next start. A pass reclaims uploads and deletes left unfinished past `sweep.stale_age` (an
hour), and handles at most `sweep.batch` (100) records. A pass the store or the database
refuses is logged at warn and retried on the next wake; it never fails the service. `/readyz`
reports the reactor as the `sweeper` check.

## Messaging

The service publishes events through [go-messaging](https://github.com/standards-lab/go-messaging)'s
runtime: a command writes its event into the Postgres outbox in its own transaction, and the
relay publishes the outbox's committed rows to NATS JetStream. In the graph (`internal/app`)
the broker is a node in the connections' layer, the runtime a node the domains Use for its
recorder, and the relay a reactor after the schema; `/readyz` reports the `broker` and
`relay` checks. With NATS away the service is unready and keeps running, its events waiting in
the outbox. The `messaging` and `nats` blocks under [Configuration](#configuration) set it up.

The service owns the `WEB_SERVICE` stream: its broker creates or updates it at start, capturing
every subject under the prefix `web-service`. Every other client, slab and the integration
harness among them, binds the stream as it finds it and writes nothing. An event's NATS subject
is its type, unchanged, so the prefix is the namespace of every type the service emits and a
consumer subscribes by type; the CloudEvents `subject` attribute is the entity the event is
about, such as the file's id. An event's type and data are public API, as a route's path and
body are: a consumer in another service binds both.

The relay holds the quiesce gate shared for each row, as the sweep does for a pass, so a schema
verb runs with it paused ([Admin](#admin)). The drain runs the server before the relay, whose
last pass publishes what was committed while the server drained; [Adding a
service](#adding-a-service) gives the rule a new producer follows.

### The event convention

- **One event per command.** Every public command that changes state commits exactly one event,
  in the transaction that makes that state true, so the event commits exactly when the state
  does. A command that fails commits none, and so does a retry of a delete already under way: a
  repeated file delete, which finds the file deleting already, or a repeated branch mark, which
  finds the directory deleting already, even when it marks a straggler. A read commits none, and
  neither do the sweep's reclamation of a marked branch or an abandoned write and the purges that
  finish a delete.
- **Seeds emit.** Every seed write that creates an entity commits the event its command
  commits, with the same data, in the transaction that creates it: an organization's
  `organization.created`, a logo's `organization.logo.uploaded` in its activation's, a
  directory's `document.directory.created`, and a file's `document.file.uploaded` in its
  completion's. A rerun over a seeded schema creates nothing and emits nothing. An
  organization's document root is created implicitly by its first write under the root alias, a
  client's or a seed's, and that write's transaction commits the root's
  `document.directory.created`; a write that finds the root, or loses the race to create it,
  commits no root event.
- **A reset emits once.** A state reset commits one `web-service.database.reset` after its
  revert and apply and before its seed, so it reaches the stream ahead of the seed's creation
  events; a consumer that holds a copy of the service's entities discards it there and rebuilds
  from what follows. The revert drops the outbox with every other table, so a reset loses the
  events still unpublished when it runs, and it emits nothing for the entities it drops. The
  schema verbs (`up`, `down`, `steps`, `force`) emit nothing.
- **Naming.** An operation on the domain itself is `<service>.<domain>.<operation>`, as
  `web-service.organization.created` is; an operation on a sub-element of the domain is
  `<service>.<domain>.<element>.<operation>`, as `web-service.document.file.uploaded` and
  `web-service.organization.logo.uploaded` are. The operation is a past-tense verb, and no type
  carries a version suffix before v1.

### The event catalog

Each event is a CloudEvent whose `source` is the configured `messaging.source` (`web-service`),
whose `subject` is the id of the entity its type names, and whose data is the JSON below. The
data carries an entity's stored state after the command, under the API's field names, never a
file's content, and never the state before: a move or a transfer carries only the new parent.
It carries no composed path, status, or timestamp. The types are defined in each
domain's `messaging.go` and their data in its `entities.go`.

| Type | Emitted by | Subject | Data |
|------|------------|---------|------|
| `web-service.organization.created` | `POST /api/organizations`; a seed's organization | organization id | `organization_id`, `parent_id` (null at the top), `code`, `name`, `version` |
| `web-service.organization.edited` | `PUT /api/organizations/{id}` | organization id | as `created`, as the edit left it |
| `web-service.organization.transferred` | `POST /api/organizations/{id}/transfer` | organization id | as `created`, under the new parent |
| `web-service.organization.deleted` | `DELETE /api/organizations/{id}` | organization id | `organization_id`, `version` (the version the row was removed at) |
| `web-service.organization.logo.uploaded` | `PUT /api/organizations/{id}/logo`; a seed's logo | logo's file id | `file_id`, `organization_id`, `content_type`, `size`, `etag`, `version` |
| `web-service.organization.logo.deleted` | `DELETE /api/organizations/{id}/logo` | logo's file id | `file_id`, `organization_id`, `version` (the version its release left it at, deleting) |
| `web-service.document.directory.created` | `POST /directories`; the document root's implicit creation; a seed's directory | directory id | `directory_id`, `organization_id`, `parent_id`, `name`, `version`; the root's `parent_id` is null and its `name` is `/` |
| `web-service.document.directory.moved` | `POST /directories/{id}/move` | directory id | as `created`, as the move left it |
| `web-service.document.directory.deleted` | `DELETE /directories/{id}` (an empty directory's removal) and `DELETE /directories/{id}?recursive=true` (a branch's mark, for the marked directory alone) | directory id | `directory_id`, `organization_id`, `version` (the removed row's, or the version the mark left it at) |
| `web-service.document.file.uploaded` | `POST /directories/{id}/files`; a seed's file | file id | `file_id`, `organization_id`, `directory_id`, `name`, `content_type`, `size`, `etag`, `version` |
| `web-service.document.file.moved` | `POST /files/{id}/move` | file id | as `uploaded`, as the move left it; `size` and `etag` are null for a pending file, whose write has not completed |
| `web-service.document.file.deleted` | `DELETE /files/{id}` | file id | `file_id`, `organization_id`, `version` (the version the delete left it at, deleting) |
| `web-service.database.reset` | `POST /admin/database/state` | the state's name | the state's name, a JSON string (`"empty"`) |

The document routes sit under `/api/documents/{org}`. A logo `PUT` that replaces a logo commits
`logo.uploaded` alone, no `logo.deleted` for the logo it replaces.

### The file protocols' hooks

A command whose change becomes true inside one of blobfs's file protocols passes the protocol a
hook, which runs in the transaction that makes the change true, where the domain commits its
event. A hook's error rolls that transaction back, so the change and the event commit together
or not at all.

- `Store.WriteFile`, blobfs's two-phase write, with `OnComplete`: the hook runs in the
  completion's transaction once the row is available, and never on a refused completion. The
  document upload runs it.
- `Store.EnsureFile`, the two-phase write's retry-safe form under a fixed id, with
  `OnComplete`: the hook runs as `WriteFile`'s does, and only when this call completes the file;
  a file found complete, or completed by another writer, runs none. The document seed's files
  run it.
- `Store.RemoveFile`, blobfs's two-phase delete, with `OnDelete`: the hook runs in the delete's
  first transaction once the row is deleting; a retry that finds the row deleting already skips
  it and finishes the purge, and the purge runs no hook. The document file delete runs it.

The logo needs none: its write and its seed complete through blobfs's own protocols, and its
event commits in the activation's transaction that follows.

### Adding an event

1. **Define the kind** in the domain's `messaging.go` with go-core's `event.Define`, its doc
   comment stating when it commits and what its subject is (`document.FileUploaded`). Its data
   type sits in `entities.go` beside the read shapes, under the API's field names, and carries
   the entity's metadata, never its content (`document.FileUploadedData`). Name the type by
   [the convention](#the-event-convention), list both in the package doc, and add the type's row
   to [the catalog](#the-event-catalog).
2. **Emit it in the committing transaction.** The domain's store holds the `Recorder` the
   composition root injects (the runtime's, `domain.go`); the command calls the recorder's `Emit`
   on the transaction that changes the state and raises the kind there with its subject
   (`emitUploaded`). The write is one more statement on that transaction, so an event that fails
   to write fails the command. A command whose state becomes true inside a blobfs file protocol
   passes it a hook and emits there ([The file protocols'
   hooks](#the-file-protocols-hooks)). A seed write that creates an entity emits as its
   command does. A domain service runs in the server's requests and needs no graph edge to the
   relay; a producer outside a request Uses the relay's node ([Adding a
   service](#adding-a-service)).
3. **Test it.** In the unit tier the domain's service is built over sqlate's scripted driver
   with go-messaging's recorder over its Postgres outbox, its id and time fixed (`recorder` in
   `domain/document/database_test.go`, stamped by `internal/outboxtest`). The outbox insert is
   one recorded statement, so a test asserts it runs once, between the completion and its
   commit, decodes it to check the type, subject, and data (`outboxtest.Decode` and
   `outboxtest.Want`), and scripts a refused insert to prove the command rolls back with no
   event committed (`outboxtest.Committed`; `domain/document/messaging_test.go`). In the
   integration tier, `integration.Subscribe` binds the stream for the kind's type before the
   command runs and returns a `Stream`, and
   `Stream.Await` waits for the event about a subject. Absence cannot be awaited, so a test
   proves a command emits nothing with a marker: after the command it runs one that does emit,
   awaits the marker's event, and asserts it arrived alone, since the relay publishes in commit
   order (`marker` and `onlyMarkers` in `integration/events_test.go`).
4. **Watch it** with `mise run slab -- events watch`, which prints each event as it arrives;
   `--from-start` replays the stream first. See [`tools/slab/README.md`](tools/slab/README.md).

## Tasks

Each task wraps a plain command, so the repository works without mise, with two caveats:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging, with no seed set, instead of the local
overlay's loopback, debug, and `default` set — set `APP_ENV=local` yourself when running without
mise. `serve`'s full command also needs a shell that understands `/dev/tcp` (bash, not `sh`); see
[`compose/README.md`](compose/README.md).

| Task | Command | What it does |
|------|---------|--------------|
| `mise run check` | per module (`.`, `tools/slab`): build, vet, `gofmt -l`, `go fix -diff`, `go mod tidy -diff`, `go test -race`, `golangci-lint`; then `go tool sqlint` | The one read-only check, run by CI; vet, fix, and lint read the integration suite too |
| `mise run currency` | `scripts/currency.sh` | Report every requirement, Go version, tool, action pin, and image tag behind its latest; writes nothing |
| `mise run upgrade` | per module: `go mod edit -go=<current minor> -toolchain=none`, `go get` latest of the direct requirements, `go mod tidy`; then `mise upgrade --bump --local` | Upgrade the go directives, the requirements, and the mise tools; actions and images move by hand from currency's report |
| `mise run vet` | `go vet -tags integration ./...`, then `go vet ./...` in `tools/slab` | Compile-check and vet, the integration suite and slab included |
| `mise run serve` | `go run ./cmd/server`, tee'd to the observability collector | Run the service locally |
| `mise run test` | `go test -race ./...`, then the same in `tools/slab` | Run the unit tier, slab included |
| `mise run integration` | an isolated `docker compose up --build`, `go test -tags integration ./integration/`, the stack's logs on a failure, `down -v` | Run the integration tier against its own stack |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./... && go tool sqlint`, then `golangci-lint run ./...` in `tools/slab` | Lint the Go and the SQL, slab included |
| `mise run db:up` | `docker compose up -d --wait --build` | Start the local Postgres, Azurite, and NATS |
| `mise run db:down` | `docker compose down` | Stop the local Postgres, Azurite, and NATS (keep data) |
| `mise run db:reset` | `docker compose down -v` | Stop the local Postgres, Azurite, and NATS and drop their data |
| `mise run otel:up` | `docker compose ... up -d --wait --build`, then polls Mimir until it can query | Start the collector, Loki, Tempo, Mimir, and Grafana |
| `mise run otel:down` | `docker compose --profile observability down …` | Stop the observability profile (keep data) |
| `mise run otel:reset` | `docker compose --profile observability down -v …` | Stop the observability profile and drop its data |
| `mise run stack:up` | `db:up` and `otel:up` | Start the local Postgres, Azurite, and NATS and the observability profile |
| `mise run stack:down` | `db:down` and `otel:down` | Stop the local Postgres, Azurite, and NATS and the observability profile (keep data) |
| `mise run stack:reset` | `db:reset` and `otel:reset` | Stop the local Postgres, Azurite, and NATS and the observability profile and drop their data |
| `mise run db:state <state>` | `curl -d '{"state":"<state>","confirm":true}' localhost:8080/admin/database/state` | Reset the running service's database to a named state |
| `mise run slab -- scenario <name>` | `cd tools/slab && go run ./cmd/slab` | Run a narrated scenario (`mise run slab -- scenario` lists them) — see [`tools/slab/README.md`](tools/slab/README.md) |

`mise run slab -- scenario storage` narrates the storage end to end against the running service: the
two migration sets, the seeded logos and document tree, the validators and refusals, and a
recursive delete waited out through the sweep.

## Tests

Two tiers. The unit tier, `mise run test`, runs on every pull request inside `mise run check`
and touches no service, network, or disk: a package that runs SQL proves it over sqlate's
scripted driver, and one that stores objects over go-storage's fake. The integration tier,
`mise run integration`, runs the composed service black-box through its API against the compose
stack, in CI on every merge to main and on demand from the Actions tab.

The suite lives in the `integration` package under the `integration` build tag. Each file is
named for the surface it asserts (`organization_test.go`, `document_test.go`, `logo_test.go`,
`sweep_test.go`, `outage_test.go`, and the rest), and each test's comment states what it proves.
Its harness is the toolkit the SDKs ship beside what it exercises: go-core's
`process/processtest` builds `cmd/server` once, runs it as a subprocess configured by `APP_*`
variables on a reserved port, and relays the database, the object store, or the broker through
a loopback forwarder (`Options.Database`, `Options.Storage`, `Options.NATS`) that a test severs
for an outage. go-web-sdk's `webtest` drives the service through its API; beneath it,
`integration.Objects` reads the container, `integration.Database` opens a session on the
database, and `integration.Subscribe` reads the service's stream ([Adding an
event](#adding-an-event)). A failing test prints each service process's output, so a 500 comes with the
service's own record of it. The service adds only its configuration and state control through
the admin mount; nothing in the service exists for the tests' sake.

The task runs the same `compose.yml` as its own project (`go-web-service-integration`, Postgres
on 5433, Azurite on 10001, and NATS on 4223), so the development stack and its data are never touched, and
tears the stack down with its volumes when the suite ends, so every run starts from an empty
database and container. A failing run prints the stack's logs before the teardown; CI's
`integration` job runs the task as it is. The harness honors `APP_DATABASE_HOST`,
`APP_DATABASE_PORT`, `APP_DATABASE_PASSWORD`, `APP_STORAGE_ENDPOINT`, `APP_STORAGE_ACCOUNT`,
`APP_STORAGE_KEY`, and `APP_NATS_URL` for a stack elsewhere.

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
   the `APP_STORAGE_*` family (below), the `APP_MESSAGING_*` and `APP_NATS_*` families
   (below), `APP_OBSERVABILITY_ENDPOINT` and
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
resumes past a try's deadline, so it runs as long as its client reads. `max_retries` sets how
many times a failed try is retried, and how many times a download's body resumes per read.
`config.json` keeps the SDK's 3, with exponential backoff from 800 ms, and sets `try_timeout` to
`5s`, below `read_idle_timeout`, so a stalled try resumes once before the store is cut off. A
store that stalls on every try is refused after about 26s (four tries and the backoff), inside
the server's 30s `write_timeout`, so the 503 reaches the client; the base configuration's test
holds that budget. The `local` overlay and the integration harness set `max_retries` to `1`, so
a store that is down is refused in about a second. `block_size` and `concurrency` size a
`Put`'s staged blocks; their product, the body azureblob reads ahead of the store, must be at
least `max_object_size`, so a store that stalls never slows the body's reads into a 408.

The server's `read_timeout` and `write_timeout` are `30s` (`config.json`, go-web-sdk's
defaults), sized for a request that moves no large body. An upload or a download sets its own
connection deadlines through go-web-sdk's `Transfer`: the timeout plus the time the body takes at
`transfer_rate` (`APP_SERVER_TRANSFER_RATE`, 65536 bytes per second), the slowest pace a client
is allowed. The body's size counts up to the route's limit (10 MiB for a document, 1 MiB for a
logo). An upload slower than that pace answers 408; a download slower than that ends short.

The `sweep` block schedules the [sweep](#sweep): `interval` (`APP_SWEEP_INTERVAL`, `30s`), the
backstop wake; `batch` (`APP_SWEEP_BATCH`, 100), the records one pass handles; and `stale_age`
(`APP_SWEEP_STALE_AGE`, `1h`), the age past which a pass reclaims an unfinished upload or delete.

The `messaging` block is go-messaging's: `source` (`APP_MESSAGING_SOURCE`, `web-service` in
`config.json`, required), the CloudEvents source of every event the service emits;
`relay_poll` (`APP_MESSAGING_RELAY_POLL`, `250ms`), the relay's wait between passes once the
outbox is empty; and `relay_timeout` (`APP_MESSAGING_RELAY_TIMEOUT`, `10s`), the bound on one
publish. The `nats` block is go-messaging's NATS provider's: `url` (`APP_NATS_URL`,
`nats://127.0.0.1:4222` in `config.json`), `name` (`APP_NATS_NAME`, the connection's name),
`stream` (`APP_NATS_STREAM`, `WEB_SERVICE`), `prefix` (`APP_NATS_PREFIX`, `web-service`, the
namespace of the service's event types), `owner` (`APP_NATS_OWNER`, `true`: the service creates
or updates its stream on start), and, on the owner only, `duplicates` (`APP_NATS_DUPLICATES`,
the stream's deduplication window) and `max_age` (`APP_NATS_MAX_AGE`, its retention, 0 keeping
every event). An invalid block fails startup, naming it.

The compose file honors `POSTGRES_PORT` for the Postgres container's published port,
`AZURITE_BLOB_PORT` for Azurite's, and `NATS_PORT` for NATS's, but the service does not read
them: moving a port off its default splits the two until the matching `APP_DATABASE_PORT`,
`APP_STORAGE_ENDPOINT`, or `APP_NATS_URL` variable follows. Postgres's user, password, and database (`app` each) are baked into
`compose/postgres/Dockerfile`; they pair with `config.json` and `secrets.example.json`, so change
both sides together.

## License

Apache 2.0, see [LICENSE](LICENSE).
