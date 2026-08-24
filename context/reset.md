# reset · roadmap-replan

- **Status:** closeout
- **Session:** plan
- **Branch:** roadmap-replan

## Disposition

- **Culled:** `concepts/roadmap.md` and `concepts/data-cqrs-roadmap.md` — sequencing now lives
  in the workspace roadmap at the coordinator (standards-lab `context/roadmap.toml`), where the
  data layer is the goal `v1.data` with its tasks, and the only sequence is `next`.
- **Integrated:** `design/end-state.md` deleted — its v1.0 definition was absorbed, rebased to
  the revised scope, into the roadmap's root goal `v1` criteria. v1.0 now spans eight
  capability layers: data/CQRS, auth and ABAC, observability, object storage, messaging and
  reactor services, AI, the embedded client, and deployment; the service is versionless until
  1.0, minors cut as layers complete in any order.
- **Added:** `concepts/data-layer.md` — the layer's design direction carried out of the culled
  notes: strategy, the settled reads direction, writes candidates, the domain model, the
  evaluation evidence, and the prior R&D. Roadmap tasks are cited by dotted path
  (`v1.data.reads`).
- **Integrated (review fixes):** `design/stack.md` no longer names the deleted
  `internal/process`; `context/README.md` re-pointed at the workspace roadmap, its capability
  map extended to the eight `v1` goals; `concepts/organization-lineage.md` cites task paths
  instead of the retired rung numbers.
- **Retained:** `design/stack.md`, `design/documented-layers.md`, `design/composition-root.md`,
  `concepts/identity-linking.md`, `concepts/organization-lineage.md` — reviewed against the
  code, no drift beyond the fixes above.

## Next-focus

Continuity follows the coordinator's roadmap `next`: the coordinator sweep and the
claude-plugins sessions first, then `v1.data.reads` — the coordinated reads slice across
go-database, go-web-sdk, and this service (`concepts/data-layer.md` holds the settled
direction).
