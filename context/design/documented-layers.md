# Documented layers

How this repository changes and releases.

- The documented layer is the unit of change. A refinement moves a layer's code, its
  documentation section, and its tests together, and every change is a marathon session, never
  ad hoc.
- The decay rule applies: a documentation section that no longer matches the code is a defect,
  fixed in the same change.
- Refinements are additive (a new layer) or modifying (a new provider, a CQRS change) — touching
  that layer's code, documentation, tests, and the seams it participates in.
- The service is the repository's only releasable artifact, on simple semantic version tags,
  versionless until its first release. Each version is a coherent snapshot: code, documentation,
  and the pinned go-core, go-web-sdk, and go-database versions it was validated against. Dev
  releases are supported between semantic releases and purged at the next minor-or-above
  release.
- A refinement that proves a better pattern promotes outward — into the SDKs
  (go-core, go-web-sdk, go-database), go-web-sdk-template, and the standard. A library change
  and the service change that proves it release as a coordinated snapshot.
