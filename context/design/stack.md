# The stack

The technologies this service composes, declared once, and the rules that keep each provider where it
belongs. Settled in the capability-tiers planning session (2026-08-17); the port list grows as layers
land.

## One declared stack

The service is one cohesive composition on one stack. It runs Postgres for SQL (18, from the compose
file locally; Azure Database for PostgreSQL or Amazon RDS when managed — the same provider, pointed
elsewhere by configuration). The auth layer will add Keycloak; the observability layer an OpenTelemetry
collector. Each is one provider of its capability, and the service never carries a second provider of
the same capability behind a switch: a variant of this service on another engine or identity provider
would be a separate repository, not a configuration option here.

## Two tiers, and where each is used

Every capability the service consumes is used at one of two resolutions, and the choice is made per
use, at the resolution the purpose requires.

- **Standard** — the library's interface that is exactly the technology's common standard: for SQL,
  ISO/IEC 9075 through the `database` wrapper and the query vocabulary; for HTTP, RFC 9110 and 9457
  through `web`. Domain packages, handlers, and the composition root work here by default.
- **Native** — the provider's own features, reached through the handle the library exposes
  (`database.DB.Conn()`) or written directly in SQL the engine defines. Native use is first-class —
  the point of choosing Postgres is to use Postgres — and it is contained: a package that uses a
  native feature declares it, wraps it, and presents the standard tier to its callers.

## The import boundary

Only three kinds of package import a provider: the composition root (`internal/infrastructure`, which
constructs the Postgres provider and hands the pool downward as a primitive), the `cmd/*` binaries, and
domain packages that declare native use. Everything else — handlers, `internal/config`, and any
domain package that stays within the standard tier — is provider-free. The boundary is a
documented convention; mechanical enforcement (`.golangci.yml`, `depguard` denying the
provider module outside the declared packages) was deliberately deferred at the first domain
package (2026-08-26) — the allowlist isn't validated against real usage yet, and an unproven
denylist would block legitimate work. `v1.data.evaluation` re-asks with the full layer's
evidence.

## What a provider swap changes

SQL is schema-bearing: the service owns a schema and domain SQL written for Postgres, so moving to
another engine is a port, not a configuration change. What changes on a swap is exactly the port list
below — never the composition root's wiring, the configuration, or the code that uses the wrapper and
the query vocabulary. HTTP has no provider. The auth and observability layers declare their class when
they land: token verification is a configuration change; what a token's claims contain needs a review;
OpenTelemetry exporters are a configuration change.

## The port list

Every native use, by package, with what a port would replace:

- `cmd/db/migrations/0001_organization` — `uuidv7()` as the id default (a Postgres 18 builtin; a port
  mints ids in the application or uses the engine's generator), `UNIQUE NULLS NOT DISTINCT` for the
  sibling-scoped code (a partial unique index or a coalesced key elsewhere), and the regular-expression
  `CHECK` on `code` (`~` is Postgres syntax). The recursive lineage query is SQL:1999 and portable.
- The migration and seed tooling in `cmd/db`: golang-migrate's pgx driver, and the `ON CONFLICT ON
  CONSTRAINT` idempotency in the organization loader.
- `domain/organization` — no native use: the recursive lineage CTE is SQL:1999 and the whole
  read path stays within the standard tier.
- Domain packages as they land, each declaring the engine-specific SQL it owns.
