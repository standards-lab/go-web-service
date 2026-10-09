# Changelog

All notable changes to `github.com/standards-lab/go-web-service` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the service adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). The service is unreleased; changes
accumulate under [Unreleased] until the first cut.

## [Unreleased]

### Added

- Object storage: the `storage` configuration block (`APP_STORAGE_*`, go-storage's), an Azure
  Blob Storage provider through `go-storage/azureblob`, Azurite in the compose file, and blobfs
  with `blobfs/postgres` for the file tree's rows, its migration set (`blobfs`) run beneath the
  service's own (`app`). The object store starts in the pool's layer, beside it.
  `/readyz` lists the `storage` check.
- The storage admin service under `/admin/storage`: `GET /diagnostics` (whether a live probe
  succeeds, the container, the provider's longest key) and `POST /container`, which creates the
  configured container when it is missing.
- The organization logo, `PUT`, `GET`, and `DELETE /api/organizations/{id}/logo`: a raw PNG,
  JPEG, WebP, or GIF body of at most 1 MiB (415 for any other type), stored through blobfs and
  bound by the `organization_image` table, one active per organization; the `GET` is served
  `no-cache` and revalidates by the object's `ETag` and `Last-Modified`. The image row is
  inserted only once its file completes, in the activation's transaction, so a write that stops
  partway leaves a plain pending row the sweep reclaims, never an image that would block the
  organization's delete. The organization path
  read moved to `GET /api/organizations/lookup?path=…`.
- The document domain under `/api/documents/{org}`: each organization's hierarchy of directories
  and files over blobfs, rooted at a document root its first write creates and bound by the
  `organization_directory` table. Directory create, read with its `status` (`active` or
  `deleting`), cursor-paged listings, moves, and deletes; file uploads (`POST
  /directories/{id}/files?name=…`) of at most 10 MiB, metadata, downloads as an attachment
  (`private, no-cache`, 304 on `If-None-Match` and `If-Modified-Since`), moves, and deletes. An id
  outside the organization's root is 404. The moves and the deletes take the row's version in
  `If-Match`; a recursive directory delete marks the branch deleting and answers 202 with the
  directory's read as its `Location`, its listings 404 and its writes 409 until the sweep removes
  it.
- The sweep: data's background worker (`data.Storage.SweepWorker`), blobfs's sweep in bounded
  passes, run on the reactor the composition root defines as the `sweeper` node. It finishes
  the branches a recursive delete marks and reclaims uploads and deletes left unfinished past
  the stale age; it wakes on each recursive delete, on an interval, and once at startup, and
  logs a refused pass at warn without failing the service. Once the service drains, it finishes
  the pass in flight and runs no further one. The `sweep` configuration block
  (`APP_SWEEP_INTERVAL`, `APP_SWEEP_BATCH`, `APP_SWEEP_STALE_AGE`; 30s, 100, 1h), and the
  `sweeper` check on `/readyz`.
- The quiesce gate: the schema-changing admin verbs (`up`, `down`, `steps`, and the state reset)
  hold it exclusively and each sweep pass holds it shared, so the two never run at once and a
  state reset never deadlocks with a pass (SQLSTATE 40P01). The gate is per process.
- The file protocols both domains run: blobfs's two-phase write with its retry-safe form for
  fixed ids and its two-phase delete (`Store.WriteFile`, `EnsureFile`, `RemoveFile`,
  `RemoveFileID`, `PurgeFile`), promoted to blobfs v0.3.0 from the copies this service staged in
  `data`, and `data.Storage.Serve`, the read of an available file as a `data.Download`, which
  stays the service's.
- The storage seeds: the `default` state stores a logo per organization and acme's document
  tree under fixed `5eed…` ids, so a rerun finds each file and a reset, which leaves the objects
  in the container, writes it again under the same key.
- `slab demo storage`, a narrated scenario of the storage end to end, and slab's `org logo`,
  `docs`, and `admin storage` commands.
- The integration tier's storage cases (`TestDocument`, `TestDocumentRefusedUpload`,
  `TestStorageOutage`, `TestDocumentSweep`, `TestSweepAtStartup`, `TestSweepUnderReset`,
  `TestOrganizationLogo`, `TestSeededStorage`),
  the object store relayed through a forwarder (`Options.Storage`) and read beneath the API
  (`integration.Objects`); a failed test prints the service's output.
- Rate limiting on the router-level middleware stack, over
  `github.com/standards-lab/go-web-sdk/middleware/rate-limit`: a client over the configured limit
  (300 requests per minute by default, `APP_RATE_LIMIT_REQUESTS`/`APP_RATE_LIMIT_WINDOW`) gets a
  429 problem document with `Retry-After`; the liveness and readiness probes are exempt.
- `tools/slab`, a narrated-scenario CLI in its own module (`mise run slab -- list`/
  `demo <scenario>`; see [`tools/slab/README.md`](tools/slab/README.md)): `sqlate` runs the
  library's compile pipeline in process over the repository's own sources; `domain` drives the
  organization domain's full CRUD surface against the running service, reseeded from the known
  reference tree each run, with the create request's trace located in Grafana; `problems` tours
  the service's RFC 9457 problem-response contract, one request per real error condition, each
  with its own trace pointer, resetting first so a run never drifts the seed data. Not a v1
  layer — the architect's own instrument for demos and self-serve exploration.
- Named database states: one file per state under `data/seeds/`, keyed by each domain's seed
  contribution (`organizations`, `logos`, `documents`), with the files a state names under
  `data/seeds/fixtures/`. `default` is the reference tree, a logo for each organization, and
  acme's document tree; `empty` is nothing. `GET /admin/database/states` lists them;
  `POST /admin/database/state` resets the database to one, every migration set reverted and
  applied, the state seeded, and answers with the transition; `POST /admin/database/seed`
  takes an optional `{"state": "…"}` to apply a named set over what is there. Both answer with
  what they stored by seed contribution: the rows commit in one transaction, then the files are
  written. `mise run db:state <state>` runs the reset against the local service.
- The integration tier: the root `integration` package, a harness that runs the built service as
  a subprocess against the compose stack and a `//go:build integration` suite asserting the
  lifecycle, the organization API, the admin mount, and the 503 on a database outage through
  the API; `mise run integration`; the CI job on merge to main and `workflow_dispatch`. The
  harness's runner, forwarder, and client then promoted to go-core v0.4.0
  (`process/processtest`) and go-web-sdk v0.7.0 (`webtest`); the package keeps the service's
  configuration and its admin-mount state control over them.
- The `data` package: the database infrastructure as the domains see it. The session grouped
  with the pattern catalog and the statements registry, the migration set under
  `data/migrations`, the application's pattern namespace (`app.identity`), the seeder behind the
  admin service, the advisory-lock name registry with `Database.Lock`, the lowering from the web
  SDK's query to the library's directives, and the shared status matcher.
- The database admin service, go-database's `admin` package, mounted under `/admin/database`:
  diagnostics, the schema status, the pattern catalog, the statements inventory, the schema
  verbs, and seed. Startup verifies the schema, applies pending migrations, and seeds when
  `admin.seed` (`APP_ADMIN_SEED`) is on.
- The `admin` configuration block with the seed switch, off by default and on in the `local`
  overlay.
- Filter operators on the collection read, `field[op]=value`, and membership from a repeated
  parameter; a malformed filter value answers 400.
- `sqlint.toml` and the `sqlint` tool: `mise run lint` and CI lint the SQL files.
- The observability compose profile (`mise run otel:up`/`otel:down`): an OpenTelemetry Collector
  alongside a local Loki, Tempo, and Mimir stack and Grafana, provisioned with cross-linked
  datasources (log-to-trace, trace-to-logs, trace-to-metrics, and exemplars once something emits
  them) and a first dashboard of the collector's own pipeline health. `mise run serve` streams
  its stdout to the collector's log receiver over TCP whenever the profile is running, a hard
  dependency accepted as a known limitation and documented in `compose/README.md`, which also
  covers the full stack's wiring.
- The observability configuration block and a composition-root telemetry node wiring the
  service to `go-observability`: its start installs the tracer and meter providers ahead of
  every other lifecycle participant and its shutdown flushes them, bounded to a short timeout,
  after the last one drains; the resource carries `service.name` (a literal, matching the
  collector's own configuration) and `service.version` from the build's VCS revision. The
  middleware chain gains tracing outermost and a request-id source drawn from the request's
  trace id, so the id in a problem document's `request_id` extension, the request logger's
  record, and the correlating log handler's `trace_id`/`span_id` attributes are all the same
  value. `log.format` defaults to `json`, which the collector's log pipeline needed all along.

### Changed

- Every time the API's JSON carries (an organization's, a directory's, and a file's
  `created_at` and `updated_at`) is in UTC, ending in `Z`, whatever zone the process runs in. It
  carried the host's offset before. The UTC guarantee comes from the libraries' releases, which
  both modules now require: go-core v0.7.0, sqlate v0.5.0 with postgres/v0.5.0 and
  sqlint/v0.3.0, go-database v0.8.0 with postgres/v0.5.0, go-web-sdk v0.15.1 with
  middleware/rate-limit v0.3.0, go-observability v0.2.0 with otlp/v0.2.0, go-storage v0.6.0
  with azureblob/v0.5.0, and blobfs v0.6.0 with postgres/v0.4.0; their own breaking changes are
  in their CHANGELOGs.
  - The log records' times are in UTC too, through go-core's logger.
  - The migration history's `applied_at` becomes `timestamp with time zone`, altered in place
    by sqlate's postgres dialect on the first schema run after the upgrade.
  - The telemetry node's shutdown releases the exporters when its start failed or never ran,
    through go-observability's own `Shutdown`, where it skipped them before.
  - The rate limit keys an IPv4-mapped IPv6 client by its IPv4 address (httprate v0.16.1,
    through rate-limit v0.3.0), where every such client shared one counter.
  - The integration tier runs every service process in `Asia/Kolkata`, +05:30 all year
    (`integration.ServiceZone`), and `TestJSONTimesUTC` asserts the API's times end in `Z`.
- The compose stack builds every service from its own `compose/<service>/Dockerfile`, whose
  `FROM` line is the service's one image pin, with its configuration (the observability YAML and
  Grafana's provisioning) baked in and, for Postgres, Azurite, and Grafana, its `HEALTHCHECK`.
  Stacks start with `up -d --wait --build`. The mise service tasks are renamed group first:
  `db:up`, `db:down`, `db:reset`, `db:state`, `otel:*`, and `stack:*`. Postgres's user,
  password, and database are fixed at `app` in its Dockerfile, so `POSTGRES_USER`,
  `POSTGRES_PASSWORD`, and `POSTGRES_DB` no longer override them. CI's `integration` job runs
  `mise run integration`, which prints the stack's logs on a failure. `mise run currency` reports
  a `FROM` line behind its image's latest tag.
- The composition root runs on go-core v0.6.0's dependency graph, which it requires with
  go-web-sdk v0.15.0 and go-storage v0.5.0; those libraries' own breaking changes are in their
  CHANGELOGs. `internal/app` describes the service as one graph, with a handle on each node in
  the exported `Nodes` value; each layer file defines its nodes in its define function. A node's
  part in the lifecycle is inferred from its value's methods, so nothing registers with the
  coordinator, and its order from the nodes it uses: telemetry first, the database and the object
  store together, the schema after both, the sweeper after the schema, and the server last, alone
  in the top layer. The drain runs in reverse, so the server drains first. `app.New` only
  describes the graph and cannot fail, so `cmd/server` no longer reports `app init failed`;
  `App.Run` builds it, and a constructor's error exits 1 as `service failed`, naming the node.
  `App.Graph` and `App.Nodes` let a caller observe the build or replace a node before `Run`.
  Adding a service is defining a node in its layer's define function; a reactor's node also
  joins `Nodes.Reactors`.
  - The configuration embeds go-core's `lifecycle.Config`, so `shutdown_timeout`
    (`APP_SHUTDOWN_TIMEOUT`, 10s) keeps its key, default, and error texts.
  - Telemetry is a graph node beneath every other lifecycle participant: its start installs
    the providers before any starts, and its shutdown flushes them after all have stopped.
  - The sweep's wake has no readiness of its own, so `/readyz` gains no check for it.
  - `/healthz` and `/readyz` are unchanged: `/readyz` reports `lifecycle`, `database`,
    `storage`, `schema`, and `sweeper`, in that order.
- The storage suite's closing releases, validated together: go-core v0.5.0, go-web-sdk v0.14.0
  with middleware/rate-limit v0.2.0, go-database v0.7.0 with postgres/v0.4.0, go-storage v0.4.0
  with azureblob/v0.4.0, and blobfs v0.5.0 with postgres/v0.3.0:
  - go-web-sdk's `PathUUID` refuses a malformed path id (`sdk.PathID`, promoted).
  - `middleware.Recoverer` answers a handler's panic with a logged 500.
  - A listing omits `total` only when it did not count. Before its first write, a document
    root's alias lists an empty page, counted on the first page as an empty root's is.
  - A download whose object fails to open answers `no-store`, with none of the file's headers.
  - `POST /admin/database/state` with an empty state resets to the configured seed.
  - Every configuration file decodes strictly: an unknown key fails the load.
  - The schema's startup checks every statement once, through the seeder, before it seeds.
    The separate `verify` step is removed.
- When a logo replacement or delete commits and its purge then fails, the request succeeds and
  logs the failure at warn; the stale reclaim finishes the file.
- A logo seed that loses its activation to another logo leaves that logo and retires its own
  file, with no error.
- An upload whose body fails answers 400, no longer a lost database connection's 503.
- Timeouts follow the Go convention. The server's read and write timeouts are `30s`. An upload
  or a download sets its own connection deadlines from its body's size and
  `server.transfer_rate` (64 KiB/s), the slowest pace a client is allowed: a slower upload
  answers 408 (`data.ErrBodyTimeout`), and a slower download ends short. `try_timeout` is `5s`
  and bounds one operation: a download's body resumes past it, and `storage.read_idle_timeout`
  (`30s`) cuts off a store that stops sending. A store that stalls on every try is refused with
  a 503 after about 26s, inside `write_timeout`. `max_retries: 1` moves from `config.json` to the
  `local` overlay and the integration harness.
- The seeder verifies every store registered on the database, including a store that seeds
  nothing: `data.Database.Register` takes the store's verifier beside its statements, and
  `data.NewStorage` takes the database and records blobfs's store, so no store can be left out
  of the check. `NewSeeder` takes no verifier list; `Contribution.Verifiers` and the domains'
  `Service.Verifier` are removed.
- Before its first write, a document root's alias refuses a bad sort, filter, or cursor with 400,
  as a real root does.
- A directory read and an empty directory's delete run their scope check in their own
  transaction. The logo's organization check is a key lookup.
- CI runs slab's vet, `go mod tidy -diff`, tests, and golangci-lint.
- golangci-lint's `testpackage` check fails on any white-box test file except `export_test.go`,
  which may only export a clock or probe hook; every test drives the exported API from `<pkg>_test`.
- The storage libraries this service validated before them: blobfs v0.4.0 with postgres/v0.3.0,
  go-storage v0.2.1 with azureblob/v0.2.0, go-web-sdk v0.12.0, and go-database v0.6.2, the
  promotions and resolution items of `v1.storage.suite`:
  - A lost container is a 503 on every storage operation, a download's read included, which
    answered 404 before; go-storage's `ErrContainerNotFound` replaces the service's workaround.
  - A file's own delete and a branch's are told apart by blobfs's `DeletingError`, with no
    re-read, the same details on the wire. A name a hidden deleting row holds answers 409 "the
    file is being deleted" (or the directory's), no longer "an entry with that name already
    exists".
  - The sweep worker runs blobfs's `SweepUntilDone` over its gated pass.
  - A download's `Content-Disposition` is go-web-sdk's `Attachment`, which carries a name
    holding a `%` in `filename*`.
  - Every error writer logs the cause of a 5xx through the service's logger, a 503 at warn and
    a client that hung up at debug.

- `admin.seed` (`APP_ADMIN_SEED`) names the state whose set applies at startup and on a
  bodyless seed, the way a deployment initializes its data; empty names none. The `local`
  overlay names `default`.
- The integration harness's `Reset` is one call to the state operation, and its `Options.Seed`
  is a state name.
- The organization domain runs on authored SQL: one `.sql` file per statement under
  `domain/organization/statements`, compiled by sqlate at construction and verified against the
  live schema at startup. Commands validate themselves on the entity types.
- Edit is `PUT /api/organizations/{id}`, full replacement; transfer requires the `parent_id`
  key, null meaning the root.
- The composition root is one file per layer under `internal/app`, as go-web-sdk-template
  v0.6.0 ships it.
- Pins: go-core v0.6.0, go-database v0.7.0 with postgres/v0.4.0, go-web-sdk v0.15.0 with
  middleware/rate-limit v0.2.0, sqlate v0.4.1 with postgres/v0.4.0, go-storage v0.5.0 with
  azureblob/v0.4.0, and blobfs v0.5.0 with postgres/v0.3.0.
- The database admin verbs act on migration sets by name: `down`, `steps`, and `force` name
  the set in their body (400 when it is missing or undeclared), and the schema status reports
  each set. The state reset requires `"confirm": true`.
- Every 409 carries a curated detail naming its kind, never the error's text: "an entry with that
  name already exists", "the directory is not empty", "the directory is being deleted", "the
  file is being deleted", "the file is referenced", or "the request conflicts with the current
  state".
- The seeder composes the domains' seed contributions: each domain seeds its own tables with
  its own statements, rows in the seed's transaction (`data.Seed`) and stored files after it
  commits (`data.FileSeed`).
- A logo `PUT` answers 201 with the new file's `{"id"}` and no version, whether or not it
  replaced a logo; the logo routes take no `If-Match`, the last write winning.
- The document root's owner row goes with its directory through a cascading foreign key, so the
  sweep needs no hook into the document domain.
- The error matchers return a `web.Problem`: `data.Status`, the database admin mount's
  matcher, and the organization domain's matcher are `web.ProblemMatcher`s, each answering a
  `Problem` carrying only the status, where they answered a bare status before.
- `RegisterHealth` takes the not-ready problem and is called with the zero `Problem`: the
  readiness answer keeps the SDK's defaults (type `about:blank`, status 503 with its status
  text as the title, the generic detail, the `checks` member) because the service names no
  problem type of its own yet.
- On the wire, an unmatched path and a wrong method answer as RFC 9457 problem documents
  instead of `net/http.ServeMux`'s plain text. go-web-sdk v0.8.0 changed the router's
  fallbacks; this service's code does not control it.

### Removed

- `cmd/db` and golang-migrate: migrations and seeding are library mechanisms the composition
  root triggers, and the admin mount exposes the former verbs.
- The `internal/infrastructure`, `internal/domain`, and `internal/reactors` packages.
- `sdk.IfMatch`, promoted to go-web-sdk v0.6.0 with the strict body decode, and `sdk.PathID`,
  promoted to go-web-sdk v0.13.0 as `PathUUID`.

[Unreleased]: https://github.com/standards-lab/go-web-service/commits/main
