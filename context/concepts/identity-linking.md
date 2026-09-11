# Identity linking

- `person.id` is the stable anchor: a UUID that is never reused. The data layer keeps it free of
  authentication meaning; it additionally serves as a `subject.id` under standards-lab
  `context/design/auth-strategy.md`'s composite foreign key, which adds authorization vocabulary, not
  authentication vocabulary.
- The link table is keyed by issuer and subject claim, unique per identity, associating an
  authentication identity with a `subject` (of which `person` is one kind, per `auth-strategy.md` §3)
  rather than directly with a person. A subject may carry more than one identity (provider migration);
  an identity maps to exactly one subject.
- The link table is where data-oriented authorization starts: permissions expressed over the composed
  data model (a person acts on records they hold custody of; unit-scoped grants) rather than over routes
  alone. `auth-strategy.md` settles that model; the data layer guarantees only the anchor.

No schema ships before `v1.data.people` builds the `person` table against the settled `subject` anchor.
