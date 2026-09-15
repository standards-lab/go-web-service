# Showing the running service off

There's no way today to hand someone a running composition and let them see what it does: the API
answers curl and the compose tasks bring the stack up, but nothing narrates a walkthrough or lets
a person explore it directly. The gap widens as more capability layers land — observability now,
messaging and its reactor services later — each adding something worth demonstrating that a plain
API client doesn't surface (a live trace in Tempo, an event a reactor picked up).

Two directions, not yet chosen between:

- **A narrated demo script** (a `mise run demo` task walking the API end to end with curl/jq,
  printing each step) — cheap, scales by appending a step per new domain, needs no new
  infrastructure. It's a fixed tour, not something a person can explore on their own.
- **An OpenAPI spec and a mounted explorer UI** — lets a person try the API themselves rather than
  watch a tour, and generalizes better as the surface grows. Costs an ongoing spec-authoring
  discipline on every route from the moment it's adopted. The architect looked at Go's OpenAPI
  tooling before and found the ecosystem weak; worth a fresh look, since this session's `go-web-sdk`
  routing and problem-document conventions may fit some generators better than others, and the
  landscape may have moved.

Observability adds a second axis: showing the system off now also means showing the telemetry it
produces (a request's trace in Tempo, its correlated log in Loki), not just its API responses.
Messaging will add a third once `v1.messaging` lands: demonstrating an event reaching a reactor
has no API-request shape at all. Whatever strategy this settles on should account for all three
from the start, or it will need a second redesign when messaging arrives.

Decide this in a `plan` session of its own, not opportunistically inside a working session on
something else.
