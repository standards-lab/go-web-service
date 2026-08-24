# Identity linking

Deferred to the auth layer. Recorded now because the data layer fixes the anchor it builds on.

- `person.id` is the stable anchor: a UUID that is never reused. The data layer keeps it free of
  authentication meaning.
- The intended shape is a link table keyed by issuer and subject, unique per identity, associating
  an authentication identity with a person. A person may carry more than one identity (provider
  migration); an identity maps to exactly one person.
- The link table is where data-oriented authorization starts: permissions expressed over the
  composed data model (a person acts on records they hold custody of; unit-scoped grants) rather
  than over routes alone. The auth layer settles that model; the data layer guarantees only the
  anchor.

No schema ships before the auth layer consumes it.
