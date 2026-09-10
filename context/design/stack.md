# The stack

The technologies this service composes, declared once, and the rules that keep each provider
where it belongs. Settled in the capability-tiers planning session (2026-08-17) and restated on
authored SQL at `v1.data.sql.integration.service` (2026-09-06); the port list grows as layers
land.

## One declared stack

The service is one cohesive composition on one stack. It runs Postgres for SQL (18, from the
compose file locally; Azure Database for PostgreSQL or Amazon RDS when managed, the same provider
pointed elsewhere by configuration). The auth layer will add Keycloak; the observability layer an
OpenTelemetry collector. Each is one provider of its capability, and the service never carries a
second provider of the same capability behind a switch: a variant of this service on another
engine or identity provider would be a separate repository.

## Two tiers, and where each is used

Every capability the service consumes is used at one of two resolutions, and the choice is made
per use, at the resolution the purpose requires.

- **Standard**: the technology's common standard. For SQL, ISO/IEC 9075 in an authored `.sql`
  file declaring `--| tier: standard`, run through sqlate; for HTTP, RFC 9110 and 9457 through
  `web`. Domain statements, handlers, and the composition root work here by default.
- **Native**: the provider's own features, written in a `.sql` file declaring `--| tier: native`
  with the feature it uses and how another engine expresses it. Native use is first-class, the
  point of choosing Postgres is to use Postgres, and it is contained: the declaration is per
  file, `sqlint` fails a standard file that uses a native form, and the admin mount reports each
  statement's tier.

## The import boundary

One package imports a provider: the composition root (`internal/app/infrastructure.go`), which
constructs the Postgres pool through go-database's provider and takes the dialect from sqlate's,
and hands the session downward as a primitive. Everything else, the `data` package, the domain
packages, the admin domains, the handlers, and `internal/config`, is provider-free in its
imports; native use is in SQL files, declared in their headers. The boundary is a documented
convention; mechanical enforcement (`.golangci.yml`, `depguard` denying the provider modules
outside the composition root) was deliberately deferred at the first domain package
(2026-08-26). `v1.data.evaluation` re-asks with the full layer's evidence.

## What a provider swap changes

SQL is schema-bearing: the service owns a schema and domain SQL written for Postgres, so moving
to another engine is a port. What changes on a swap is exactly the port list below, plus the two
provider imports at the composition root; never the configuration or the code around the
statements. HTTP has no provider. The auth and observability layers declare their class when
they land:

  - token verification is a configuration change
  - what a token's claims contain needs a review
  - OpenTelemetry exporters are a configuration change

## The port list

The port list is the set of files declaring `--| tier: native`, each naming its port in its
header, plus the migrations, which are engine DDL by nature:

- `data/migrations/0001_organization`: `uuidv7()` as the id default (a Postgres 18 builtin; a
  port mints ids in the application or uses the engine's generator), `UNIQUE NULLS NOT DISTINCT`
  for the sibling-scoped code (a partial unique index or a coalesced key elsewhere), and the
  regular-expression `CHECK` on `code` (`~` is Postgres syntax).
- `data/patterns/identity.sql`: RETURNING for the identity every create returns; a port
  respells the one pattern (OUTPUT INSERTED, RETURNING INTO, or an insert plus a read).
- `data/statements/lock.sql`: `pg_advisory_xact_lock` over `hashtext`, the service's one
  advisory lock (a port swaps in the engine's application lock or a `FOR UPDATE` mutex row).
- `data/statements/seed_organization.sql`: `ON CONFLICT ON CONSTRAINT ... DO NOTHING` with
  RETURNING for the idempotent seed (MERGE or INSERT IGNORE elsewhere).
- `domain/organization/statements/create.sql`: native through the identity include only.

The read path, the lineage CTE, and the guarded commands (`CURRENT_TIMESTAMP`, the version
predicate) stay within the standard tier. Domain packages as they land add their native files.
