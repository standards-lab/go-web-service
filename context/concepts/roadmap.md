# Roadmap

The ordered path to the end state in `design/end-state.md`, at low resolution: one paragraph per
milestone, and only the live layer detailed anywhere. Captured in the capability-tiers planning session
(2026-08-17); each milestone's plan session settles its own detail when it is reached.

- **v0.1 — data composition and CQRS.** The live layer; its ladder is `concepts/data-cqrs-roadmap.md`.
  Rungs 1 and 2 are built (connectivity, migrations and seeding); rung 3 is reads; rung 4 writes;
  rungs 5–7 the remaining domains; rung 8 the toolchain evaluation, which under the tiers also audits
  where the service uses native features and whether each is contained; then the closing releases and
  the layer's `docs/` page.
- **v0.2 — auth and ABAC.** Keycloak as the identity provider; identity linking on the `person` anchor
  (`concepts/identity-linking.md`); verb-keyed attribute-based access control over the composed model.
  This is where unit-scoped grants need subtree and ancestor tests on the organization tree, which is
  the trigger for the lineage refinement held in `concepts/organization-lineage.md`.
- **v0.3 — observability.** Logs, metrics, and traces through OpenTelemetry, with the collector in the
  compose stack; the request logger's open questions in `web` are answered against OpenTelemetry's
  conventions.
- **v1.0 — hardening and documentation.** The three layers reviewed as one composition; the `docs/`
  tier complete, including the page naming each capability's class and the port list; the
  import-boundary lint; the coordinated releases.
- **v1.x — capability layers.** Messaging (NATS), object storage, and AI, one documented layer each,
  under the same per-layer discipline.
