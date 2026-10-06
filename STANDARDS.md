# go-web-service standards

The judgement calls the standards-reviewer applies to go-web-service, beyond what `mise run check` enforces.

- The documented layer is the unit of change: a capability lands its code, its README section, and its tests in one change, complete before the next begins; whether additive (a new layer) or modifying (a new provider, a CQRS change), it also brings current the boundaries the layer shares with other layers, and a README section or `doc.go` it leaves stale is its defect.
- The domain layer is a compositional grouping, not an entity: each `domain/<layer>` is one package with one Domain Service (`service.go`'s `Service`) and one handler (`handler.go`'s `Routes`), however many entities or integrations it holds.
- A domain has one capability-named translation file per infrastructure integration, `database.go` for its SQL and `storage.go` for blobfs; `service.go` imports neither library.
- Cross-domain coupling runs downward as an SQL check in the consumer's transaction (document's `organization_exists`, a foreign key the backstop) and upward as an interface the consuming domain declares and the composition root injects (`document.Sweeper`, satisfied by the sweep's waker).
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no provider in a base, across the root module's and `tools/slab`'s `go.mod`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of the service's and slab's packages, and the harness rules the `integration` package follows over the SDKs' toolkits.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: `cmd/server`, `internal/app`'s layer files, the root-level `data`, `domain/<layer>`, `admin/<service>` and `sdk` packages, and the `tools/slab` module.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the root module's `CHANGELOG.md`, the check over both modules, and currency.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`: `cmd/server`, `internal/app/stages.go`, and the sweep's reactor, monitored at its stage.
- `architecture/standards/go-elemental/principles/timeouts.md`: each body-moving route's `web.Transfer` from `Config.Transfer` at its body limit (`maxLogoBody`, `maxFileBody`), and `TestConfig_BaseStorageBoundsFitTheServer`.
- `architecture/standards/go-elemental/principles/dsl-driven-services.md`: every `.sql` file under `domain/*/statements` and `data/patterns`, and `sqlint.toml`; a domain defines no pattern of its own.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `"version"` passed to `Statement.Guarded`, and the reads configuration's `web.Limits` handed to every handler.
- `architecture/principles/service-tiers.md`: provider imports confined to `internal/app/infrastructure.go` and the integration harness, and the README's Stack section as the declared boundary and port list.
- `architecture/principles/composition-root.md`: `internal/app` hands each domain service the infrastructure fields it uses, never the `Infrastructure` struct.
- `architecture/principles/validation-first.md`: the configuration's Finalize, each command's `Validate` before its store call, and `Seeder.Verify` at the schema stage.
- `architecture/principles/context-architecture.md`: the README and each `doc.go` are the homes.
