# The end state

What version 1.0 of this service is, and what every documented layer must show on the way there.
Settled in the capability-tiers planning session (2026-08-17); the ordered path is the volatile
`concepts/roadmap.md`.

## Version 1.0

One cohesive service on the declared stack (`design/stack.md`) with the three documented layers the
capability map names — data composition and CQRS, auth and ABAC, observability — complete in code,
tests, and a `docs/` page each; the import-boundary lint in place; the coordinated releases cut (the
libraries at their minor, the template re-pinned, this service at 1.0); and a `docs/` page that names
each capability the service consumes, its class, and the port list. Later versions add capability
layers one at a time under the same discipline: messaging as the exemplar of a capability whose swap
needs a behavior review, object storage as the exemplar of one that swaps by configuration, and AI. The
client, the infrastructure code, and the .NET mirror are their own repositories, seeded from what 1.0
proves.

## What every layer shows

A layer is complete when it demonstrates, in the running composition:

- the standard tier used throughout, and at least one native use, contained in a declared package and
  entered in the port list;
- the class of each capability it consumes, with the sentence "what changes on a provider swap: …";
- the tests and the `docs/` page the layer's documentation discipline requires
  (`design/documented-layers.md`);
- the promotion evaluation: which of the layer's patterns proved general enough to move into the
  libraries or the template, and which stayed here because they are this service's own.
