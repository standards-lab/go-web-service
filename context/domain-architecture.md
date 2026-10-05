# Domain architecture

This note holds the rules every domain layer is built by. The organization and document packages
are the domain layers; a rule leaves this note once it lands in the architecture repository.

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
  domain's name with the store as its verifier, binds each statement to a typed handle (a
  projection for the read model, rows for a scan, a guard for a version-checked command), and
  exposes each operation as a store method that reads as what it does. Translation files are capability-named, one per
  infrastructure integration (`storage.go` in both domains; `messaging.go` and `ai.go` are planned). A
  service never touches an infrastructure API outside its translation file.
- `service.go`: the single domain service, a concrete type constructed from the `data` package.
  Its methods map endpoints to operations one to one, and each operation delegates whole to the
  store after the command's own validation. A domain declares no lifecycle stage and imports no
  part of go-core's `lifecycle`.
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
  not-null constraints stay unmatched backstops: a breach on either one is a server fault,
  reported as a 500.
- The group's error writer composes three vocabularies, first match winning:
  - the SDK maps its own request errors (400, 413, 428)
  - the layer's matcher maps only its own errors (`ErrValidation` to 400, `ErrCycle` to 409);
    a malformed path id is the SDK's own `web.PathError`
  - `data.Status` maps the library's (directives 400, the missing row 404, unique and
    foreign-key violations 409, the stale version 412, an outage 503)

  A layer contributes only its own errors and never copies the shared switch.
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

`internal/app/domain.go` constructs each layer's service from the `data` package and never hands the
`Infrastructure` struct down. A domain declares no lifecycle service. The seeder verifies
every store that runs statements: `data.Database.Register(name, stmts, verifier)` records each
domain's store with its statements, and `data.NewStorage(db, …)` records blobfs's store, so
`Seeder.Verify` checks every recorded store, including one that seeds nothing, and no list in the
composition root can leave a store out. The admin service runs `Verify` at the schema stage before
it seeds. Every stage the process uses is named once in
`internal/app/stages.go`, in dependency order: `stageInfrastructure` (the pool and the object
store), `stageSchema` (the schema, the statements, and the seed), `stageReactors`, and `stageRoot`
(the server). Each layer file registers at a stage from that table, so the ordering is the root's
alone: a stage stays at the call site, never in a library or a domain. The base layers (`data`,
`domain/<layer>`, `admin/<service>`) are root-level packages because the domain packages import
`data` and the topology-and-naming principle forbids a root-level package importing `internal/*`. A
domain that seeds the named states declares each contribution over its own tables and statements,
returned by a method of its service (`Seed`, `LogoSeed`). A contribution of rows is a `data.Seed`,
applied in the seed's one transaction. A contribution of stored files is a `data.FileSeed`: blobfs's
two-phase write puts an object outside any transaction, so the seeder runs it after the row
transaction commits. Each file goes through blobfs's `Store.EnsureFile`, the two-phase write's
retry-safe form, under a fixed id the state file carries. A rerun then finds the file, and a reset,
which leaves the container's objects in place, writes it again under the same key. A file seed
leaves alone what it does not own. `admin.go` hands every contribution to `data.NewSeeder` in the
tables' dependency order, so the domain is constructed before the admin layer and `data` names no
domain's table. `mountAPI` mounts each layer's group and hands policy at the construction site: the
service-owned reads configuration yields the one `web.Limits` every handler constructor receives,
and the server configuration yields the transfer factory (`Config.Transfer`) a layer that moves
bodies applies to its own limit. Per-layer policy variation is different values at different
construction sites.

## The sdk staging package

Library promotion candidates stage in the base `sdk` package: flat, a package meant to empty out
accumulates no sub-packages, with each file named for the library its contents are bound for.
Staging is cheap and deliberate; the `v1.data.evaluation` task rules on every tenant. The tenants
are `Command`, the guarded-command read composing the SDK's `PathUUID` (promoted from here in
go-web-sdk v0.13.0), `IfMatch`, and `DecodeJSON`, bound for go-web-sdk; and the reactor
(`reactor.go`, with the `Every` and `Wake` sources) with the quiesce gate (`gate.go`, a
context-aware readers-writer gate that prefers its exclusive side), both bound for go-core.

## The operation-shape principle

The target SQL is the design authority. For any operation, the statement an expert would
hand-write is the authored artifact itself, a file under `statements/`, and the library's only
job is what SQL cannot express: binding, composing the collection read against the header's
fields, mapping rows, verifying the text against the schema, and owning the transaction. A
shared SQL shape is a pattern the application publishes (`data/patterns`, the `app` namespace)
or the library does (`sql`), spliced at compile time; a domain defines no patterns. The test for
any Go around a statement: would it change the SQL I would have written by hand?

A library change is worth pausing a session for only when the consumer cannot correctly express
the operation through exported API; an inelegant-but-expressible shape stages in `sdk`.

## Promotion candidates

The document layer is the second domain layer, and it proves three rules, recorded for promotion
to the architecture repository at the storage lane's fold:

- The domain layer as a compositional grouping (one package, one Domain Service, one handler)
  is a candidate Go Elemental expression.
- The capability-named translation file is the in-package counterpart of the Elemental
  Architecture's downward-dependency rule; both domains' `storage.go` hold it.
- Cross-domain coupling runs two ways: downward as an SQL check in the consumer's transaction
  (the document layer's `organization_exists` inside the root's transaction, a foreign key the
  backstop), upward as an interface the consuming domain declares and the composition root
  injects (`document.Sweeper`, satisfied by the sweep's waker).

## Deferred by design

Multi-entity role refinements belong to the first multi-entity layer. Soft delete as the
standard's convention (`data-layer.md`) waits for the first domain that needs it. The
holistic operation pass over the whole architecture is `v1.data.evaluation`.
