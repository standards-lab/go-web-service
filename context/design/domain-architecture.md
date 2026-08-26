# Domain architecture

The service's domain-layer layout standard, settled and validated in the `organization-reads`
session (2026-08-26) by the organization package — the template every domain layer follows. The
code expresses the organization instance; this note carries the rules the next layer is built
by, and decays per rule as the code comes to express each one more than once.

## The domain layer

The domain **layer** — one feature layer of the web service — is the architectural unit, not
the entity. Each layer is one Go package under `domain/`, at the module's base package layer,
encapsulated by exactly one domain service and one handler regardless of how many entities or
infrastructure integrations the layer comes to hold. `internal/` remains the composition root
only.

## Role-aggregated files

A domain package holds the whole layer in role-named files, each aggregating its role for the
layer:

- `doc.go` — the layer's architectural statement.
- `entities.go` — every data structure of the layer.
- `database.go` — every go-database translation: projections, directive lowering, scanning,
  exposed only as complete operations. Translation files are **capability-named** — one per
  infrastructure integration, named for the library it adapts (`storage.go`, `messaging.go`,
  `ai.go` to come). A service never touches an infrastructure API outside its translation file.
- `service.go` — the single domain service: a concrete type (no interface until a consumer
  needs one), constructed from exactly the infrastructure fields it uses, its methods the
  direct map from endpoint to operation, every operation delegated whole to a translation
  file.
- `handler.go` — the single handler: the layer's route group, every rejection an RFC 9457
  problem through the sdk package.

Overflow: a role file that outgrows navigability splits by concern with the role kept as
prefix (`database_projections.go`) — never per-entity quartets. Sub-package-per-role is
rejected while the single-package shape holds: it forces either an entities package or
duplicated persistence models (the entity import cycle), and it exports every internal seam of
an importable base package. Revisit if a translation layer ever needs its own tests or
declaration boundary.

Naming inside the translation file: `select<Entity>` / `select<Entities>` for the operations,
one `scan<Entity>` per entity scanning in the projection's column order, one column list as
the single source of ordering with computed fields last. Operation functions take the database
as a parameter, keeping the file a service-shape-free adapter.

## The boundary

No statement, connection, or query type crosses out of a translation file. The service surface
and the handlers work at the web contract; the projection is the read contract's single field
vocabulary, so an unknown sort or filter name is a typed rejection before any SQL renders. On
the wire, a problem's detail carries error text only on a 400, where it is request-shaped and
client-actionable; any other status sends the bare title.

## The web layer

The term is **handler**. A handler builds a route **group**, not a module: the module layer is
the API itself. `internal/app/routes.go` composes the one `/api` module; each domain mounts its
group into it at a plural-resource root (`/organizations`) and may nest sub-paths beneath it.
Health stays router-level, outside the module.

## Composition wiring

`internal/domain.New` constructs each layer's service from infrastructure fields — never the
`Infrastructure` struct. `routes(dom, cfg)` mounts each layer's group and hands policy at the
construction site: the service-owned reads configuration (pointer fields, defaults at
Finalize) yields the `web.Limits` each handler constructor receives. Per-layer policy
variation is different values at different construction sites.

## The sdk staging package

Library promotion candidates stage in the base `sdk` package: flat — a package meant to empty
out accumulates no sub-packages — with each file named for the library its contents are bound
for (`web.go` → go-web-sdk, `database.go` → go-database). Staging is cheap and deliberate; the
`v1.data.evaluation` task rules on every tenant, and the writes planning session consolidates
the current inventory (`concepts/sdk-promotion.md`).

## The operation-shape principle

The target SQL is the design authority, not the existing scaffolding. For any operation, start
from the statement an expert would hand-write; an API layer earns its place by rendering
exactly that, and reuse is legitimate only when it falls out of well-factored layers without
bending the emitted SQL toward another operation's shape. The test: *would this abstraction
change the SQL I'd have written by hand?* The stack this sorts into:

1. **The AST** (go-database `query`): models any standard statement faithfully.
2. **Operation shapes**: each named operation owns its idiomatic statement — the paginated
   list is count + page over one shared WHERE; the single-row read is a bare select + WHERE
   with no ordering, paging, or count; each future write its own shape.
3. **Computed-field patterns** (`sdk.RecursivePath`): reusable builders for standard SQL
   shapes, declared as spec values.
4. **Execution generics** (`sdk.Scan`, the Select executors): statement + execution + scan,
   one per operation shape.
5. **The domain translation file**: vocabulary only.

A library change is worth pausing a session for only when the consumer *cannot correctly
express* the operation through exported API; an inelegant-but-expressible shape stages in
`sdk`.

## Elemental Architecture implications

Held here until promoted to the landing zone:

- The domain layer as a compositional grouping — one package, one Domain Service, one handler
  — is a Go Minimal expression candidate.
- The capability-named translation file is the in-package counterpart of EA's
  downward-dependency rule.
- **Open tension:** EA's element definition says a Domain Service "anchors exactly one Entity"
  (and rejected "feature" as an element on that basis). The one-service-per-layer standard
  amends that the moment a multi-entity layer lands. Organization is single-entity, so nothing
  conflicts yet; the people or inventory session — or a coordinator-level EA amendment —
  settles it deliberately.

## Deferred by design

Command DTO placement (`entities.go` vs a `commands.go` role file) is the writes session's
decision. Multi-entity role refinements belong to the first multi-entity layer. A holistic
operation pass over the whole architecture is planned once the writes features are locked in.
