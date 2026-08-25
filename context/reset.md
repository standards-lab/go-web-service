# reset · reads-replan

- **Status:** closeout
- **Session:** plan
- **Branch:** reads-replan

## Disposition

- **Cross-repo:** `v1.data.reads` promoted from a task to a goal in the workspace roadmap
  (standards-lab `context/roadmap.toml`, its own commit at the coordinator on the sibling
  `reads-replan` branch, stacked on the open `marathon-roadmap` branch). Three session-scoped
  tasks under it — `v1.data.reads.query` (go-database's query package),
  `v1.data.reads.web` (go-web-sdk's page/size/sort parsing and list envelope), and
  `v1.data.reads.organization` (this service's organization domain package, sdk staging
  package, and depguard lint) — and `next` now sequences those three. Sibling tasks stay at
  claim resolution.
- **Added:** the "Service layout" section in `concepts/data-layer.md` — `internal/` is the
  composition root only (construct, register, mount; no domain infrastructure); domain packages
  live at the base package layer with their handlers, `domain/organization` versus a root
  package settled at the organization task; promotion candidates stage in a base `sdk` package
  (first: domain-error-to-problem mapping) and promote to the SDKs when their design settles;
  the template's `internal/domain` doc comment is flagged as a promotion candidate once the
  layout is proven.
- **Integrated:** `design/composition-root.md` no longer says domain services land *in*
  `internal/domain` — they are defined in base-layer packages and constructed there;
  `concepts/organization-lineage.md` cites `v1.data.reads.organization` for the section that
  decays when the code lands.
- **Retained:** `concepts/data-layer.md` writes/domain/evaluation sections,
  `concepts/organization-lineage.md`, `concepts/identity-linking.md`, and the `design/` notes —
  unchanged beyond the fixes above.

## Next-focus

`v1.data.reads.query` — a `start` session on go-database: the query package per
`context/concepts/query.md` (directives, projection, standard-SQL builder, typed unknown-field
error), unit-proven SQL generation. The coordinator's roadmap `next` carries the full sequence
(query → web → organization).
