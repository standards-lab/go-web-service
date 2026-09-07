# Domain architecture

The service's domain-layer layout standard, settled in the `organization-reads` session
(2026-08-26), carried through the writes slice, and rewritten onto authored SQL at
`v1.data.sql.integration.service` (2026-09-06) by the organization package, the template every
domain layer follows. The code expresses the organization instance; this note carries the rules
the next layer is built by, and decays per rule as the code comes to express each one more than
once.

## The domain layer

The domain **layer**, one feature layer of the web service, is the architectural unit, not the
entity. Each layer is one Go package under `domain/`, at the module's base package layer,
encapsulated by exactly one domain service and one handler regardless of how many entities or
infrastructure integrations the layer comes to hold. `internal/` remains the composition root
only. A domain package imports the `data` package for the database and never a provider.

## Role-aggregated files

A domain package holds the whole layer in role-named files, each aggregating its role for the
layer:

- `doc.go`: the layer's architectural statement.
- `entities.go`: every data structure of the layer, the commands included. Each command owns
  its `Validate` method, and the entities' tags are the scan and binding contract: a `json` tag
  names the column a field scans from and the parameter a field binds to, and a `db` tag
  overrides it when the two differ.
- `statements/`: one authored `.sql` file per statement, named for its operation, never its SQL
  verb, with the `--|` header declaring its tier, the engine feature a native file uses and its
  port, whether it requires a transaction, and, for a read model, its key and the fields a
  request may filter and sort by.
- `database.go`: the SQL client, the sole importer of the query library. It compiles
  `statements/` against the catalog the `data` package holds, registers the inventory under the
  domain's name, binds each statement to a typed handle (a projection for the read model, rows
  for a scan, a guard for a version-checked command), and exposes each operation as a store
  method that reads as what it does. Translation files are capability-named, one per
  infrastructure integration (`storage.go`, `messaging.go`, `ai.go` to come). A service never
  touches an infrastructure API outside its translation file.
- `service.go`: the single domain service, a concrete type constructed from the `data` package,
  registering its statement verification at the domains' lifecycle stage, its methods the direct
  map from endpoint to operation, every operation delegated whole to the store after the
  command's own validation.
- `handler.go`: the single handler, the layer's route group of error-returning handlers over the
  group's error writer.

Overflow: a role file that outgrows navigability splits by concern with the role kept as prefix
(`database_reads.go`), never per-entity quartets. Sub-package-per-role is rejected while the
single-package shape holds: it forces either an entities package or duplicated persistence
models (the entity import cycle), and it exports every internal seam of an importable base
package.

## The boundary

No statement, session, or query type crosses out of the translation file. The service surface
and the handlers work at the web contract. The read model's header is the read contract's
single field vocabulary: an unknown sort or filter field, an operator the library does not
support, or a value the engine cannot cast to the field's declared type is a typed 400 before or
at the engine, never a 500. Which statuses carry a problem's detail on the wire is go-web-sdk's
`ErrorWriter.Detail`, plus whatever statuses a layer's writer adds.

## The command side

Settled at `v1.data.writes.organization` and restated on go-web-sdk v0.6.0:

- A guarded command handler reads its inputs in one order, the path id, the If-Match
  precondition, then the strictly decoded body (`sdk.Command`), then calls the service. Commands
  return identity and version only; create answers 201 with a Location header, delete 204.
- The verb follows the operation's contract. An edit is full replacement of the client-mutable
  descriptive fields, which is PUT's contract, so it is `PUT /{id}`. An action is a named
  transition with its own protocol, never an edit, so it is a POST on its own path
  (`POST /{id}/transfer`); the people domain's activate, deactivate, and transfer-unit follow it.
  A body that omits a key a structural move requires is rejected, never defaulted.
- Validation places by kind: the handler owns transport syntax (the SDK's path, precondition,
  and body errors), the command's `Validate` owns what it can know from its own fields (rules
  mirroring the schema's checks), the store owns existence and uniqueness as constraint
  violations and any check that needs SQL (the transfer cycle walk). The database's check and
  not-null constraints stay unmatched backstops: a breach is a 500, not a client error.
- The group's error writer composes three vocabularies, first match winning: the SDK maps its
  own request errors (400, 413, 428); the layer's matcher maps only its own errors (`ErrValidation`
  and `sdk.PathError` to 400, `ErrCycle` to 409); `data.Status` maps the library's (directives
  400, the missing row 404, unique and foreign-key violations 409, the stale version 412, an
  outage 503). A layer contributes only its own errors and never copies the shared switch.
- A transaction that must not interleave with another on the same structure takes the
  structure's advisory lock first through `data.Database.Lock`, by the name the `data` package's
  registry declares; a domain declares no lock of its own.

## The web layer

The term is **handler**. A handler builds a route **group**, not a module: the module layer is
the API itself. `internal/app/domain.go` composes the one `/api` module; each domain mounts its
group into it at a plural-resource root (`/organizations`) and may nest sub-paths beneath it.
Health stays router-level, outside the module. The list read's grammar is go-web-sdk's read
contract (`ParseQuery`); the lowering to the read model's header is `data.Directives`.

## Composition wiring

`internal/app/domain.go` constructs each layer's service from the `data` package and registers
it on the coordinator at the domains' stage, never handing the `Infrastructure` struct down.
The base layers (`data`, `domain/<layer>`, `admin/<service>`) are root-level packages because
the domain packages import `data` and the topology-and-naming principle forbids a root-level
package importing `internal/*`.
`mountAPI` mounts each layer's group and hands policy at the construction site: the
service-owned reads configuration yields the `web.Limits` each handler constructor receives.
Per-layer policy variation is different values at different construction sites.

## The sdk staging package

Library promotion candidates stage in the base `sdk` package: flat, a package meant to empty out
accumulates no sub-packages, with each file named for the library its contents are bound for.
Staging is cheap and deliberate; the `v1.data.evaluation` task rules on every tenant. The
reads-era inventory landed in the libraries and the template, and the If-Match parse and the
strict body decode landed in go-web-sdk v0.6.0. The tenants now are `PathID`, the typed
path-value parse, and `Command`, the guarded-command read composing it with the SDK's
`IfMatch` and `DecodeJSON`, both bound for go-web-sdk.

## The operation-shape principle

The target SQL is the design authority. For any operation, the statement an expert would
hand-write is the authored artifact itself, a file under `statements/`, and the library's only
job is what SQL cannot express: binding, composing the collection read against the header's
fields, mapping rows, verifying the text against the schema, and owning the transaction. A
shared SQL shape is a pattern the application publishes (`data/patterns`, the `app` namespace)
or the library does (`sql`), spliced at compile time; a domain defines no patterns. The test for
any Go around a statement: would it change the SQL I would have written by hand? The strategy
record is `standards-lab context/design/dsl-driven-services.md`.

A library change is worth pausing a session for only when the consumer cannot correctly express
the operation through exported API; an inelegant-but-expressible shape stages in `sdk`.

## Elemental Architecture implications

Held here until promoted to the landing zone:

- The domain layer as a compositional grouping, one package, one Domain Service, one handler, is
  a Go Elemental expression candidate.
- The capability-named translation file is the in-package counterpart of EA's
  downward-dependency rule.
- Resolved (2026-09-03): EA's element definition said a Domain Service "anchors exactly one
  Entity". The `v1.data.sql.prototype` review amended it: a Domain Service anchors a domain, a
  composition of one or more Entities, and "exactly one" was the single-entity special case. The
  docs pass lands the amendment in the landing zone.

## Deferred by design

Multi-entity role refinements belong to the first multi-entity layer. Soft delete as the
standard's convention (`concepts/data-layer.md`) waits for the first domain that needs it. The
holistic operation pass over the whole architecture is `v1.data.writes.operations`.
