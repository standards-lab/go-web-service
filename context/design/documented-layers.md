# Documented layers

How this repository changes and releases.

- The documented layer is the unit of change. A refinement moves a layer's code, its
  documentation section, and its tests together, and every change is a marathon session, never
  ad hoc.
- The decay rule applies: a documentation section that no longer matches the code is a defect,
  fixed in the same change.
- Refinements are additive (a new layer) or modifying (a new provider, a CQRS change) — touching
  that layer's code, documentation, tests, and the boundaries it shares with other layers.
- The service is the repository's only releasable artifact, versionless until its first
  release. The release discipline (coherent snapshots with their pins, prerelease tags, a
  library change and the service change that proves it releasing together) and the promotion
  rule are the roadmap's `goals.v1` criteria and the architecture repository's release-and-ci and
  independent-releases principles.
