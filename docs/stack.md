# Stack

The service is one composition on one declared stack, with one provider per capability. A variant
on another engine or identity provider would be a separate repository, never a switch inside
this one.

- SQL: Postgres 19, through [go-database](https://github.com/standards-lab/go-database) for the
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
  [`compose/README.md`](../compose/README.md). The service exports traces and metrics over OTLP and
  structured JSON logs correlated to them by trace id, all through
  [go-observability](https://github.com/standards-lab/go-observability).

## Standard and native tiers

Each use of a capability runs at one of two tiers, chosen per use. The standard tier uses the
technology's common standard: ISO/IEC 9075 SQL in an authored `.sql` file that declares
`--| tier: standard`, and RFC 9110 and RFC 9457 through go-web-sdk's `web` package. Domain
statements, handlers, and the composition root use the standard tier by default. The native
tier uses the provider's own features, in a `.sql` file that declares `--| tier: native` and
names the feature and how another engine expresses it. Native use is first-class, because the
point of choosing Postgres is to use it, and it is contained: each file declares its own tier,
`sqlint` fails a standard file that uses a native form, and the admin mount reports each
statement's tier.

`sqlint.toml` mirrors the runtime catalog the composition root builds
(`internal/app/infrastructure.go`), so the linter compiles every statement exactly as the service
does: the engine is sqlate's PostgreSQL, against its native forms, and the sources are sqlate's
patterns under `sql`, blobfs's under `blobfs`, and the application's `data/patterns` under `app`.
Each role (statements, patterns, migrations) names its directories and the checks that apply.

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

## What a provider swap changes

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
