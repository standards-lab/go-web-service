# Tests

slab's tests are black-box, each in its package's `<pkg>_test` package, and run in the unit tier
inside `mise run check`: they touch no running service, broker, or Grafana. Each test's and
subtest's name states what it proves; this page holds the reasoning the names can't carry.

## The composition root

`internal/app/graph_test.go` builds the graph without starting it, so it needs nothing running:
the broker, service, and grafana up nodes only bind clients, which the lifecycle's Start
connects or probes. It checks that each `Nodes` field names its node (the name lowercased,
spaces ignored, so `GrafanaUp` holds "grafana up"), and that every leaf of the App's own tree,
not a copied list, builds, from the nodes its path declares, a System that drops no `After` and
puts grafana up above service. A dropped `After` means a leaf forgot to declare a node something
it builds is ordered against, so that order is lost for its run. grafana up sits above service
because the lifecycle stops at the first layer with a failure: with both down, a run would
report Grafana's and never probe the service. The guard returns an error carrying the System's
layer table, and a second test runs it against fixtures that break it, so it is shown to fail.

## Direct commands

The direct command families run against `service`, a fake that answers every request with one
canned reply, a problem document for an error status, and records what it received. A test that
needs color runs on `internal/termtest`'s terminal: under `go test` the process's stdout is never
one. termtest's own test lives beside it rather than in `style`, because a root-level package's
tests import nothing under `internal/`.

## Scenarios

The scenario tests run a scenario end to end on go-cli-sdk's runner, with the service and
Grafana at an `httptest` listener reached through `SLAB_BASE` and `SLAB_GRAFANA`, from a working
directory under the repository, so a step that reads the repository reads the real seed file and
fixtures. The run closes the fake's listener before returning, so a test reads the fake's state
without its lock. A fake records each request's line with its precondition headers, so a test
checks what the steps sent, and a request no route answers fails the test. sqlate needs nothing
running, so it runs over the real repository alone.

Each fake answers the way the service does: the checks its handler makes, in its order, a problem
document for every refusal with the service's wording, and the trace id in `X-Request-Id`, as the
request-id middleware sets it. `orgFake` serves the organization surface and Grafana's health
probe on one listener, their routes not overlapping; its list ignores the filters, since no test
reads a queried list's items. `storageFake` serves the schema status, the seed and reset, acme's
logo, and acme's document tree, seeded from `data/seeds/default.json`, which it decodes on its own
so the counts and ids a scenario checks aren't the scenario's own arithmetic. Its knobs drive the
unexpected-response tests: `fault` answers one request line with 500, `takenDetail` replaces the
curated detail of a taken name, `miscount` changes what a reset reports seeding, and
`descendingWalk` serves a paged listing in descending order.

The fakes restate these service values, and change with them:

- `serviceCodePattern`: `domain/organization`'s `codePattern`.
- `serviceMaxPageSize`: `internal/config/reads.go`'s `defaultReadsMaxSize`.
- `serviceMaxCommandBody`: `domain/organization/handler.go`'s `maxCommandBody`.
- `serviceDetailNameTaken`: `data.DetailNameTaken`.

A recursive delete marks the branch, and a read of it answers deleting for
`deletingReadsBeforeSweep` reads before the fake sweeps it, so a scenario sees the deleting
status and its wait polls at least once. `eventsFake` holds any number of organizations, creates
an organization's root on its first document write, names a marked directory in `Location` by its
id, the root's too, as the service does, and logs the event type each successful command
publishes under [Adding an event](../../docs/messaging.md#adding-an-event)'s catalog, so a test
checks that the events scenario makes the events its narration names. A run that fails before
its organization's delete is followed by one against the same fake, which refuses a code a
sibling carries, so the test shows every name a run creates is its own.

`sweeperFake` is an `eventsFake` with Grafana's Mimir datasource proxy beside it. A `passes` key
naming a reclaimer alone counts its drained passes, and `reclaimer/outcome` another outcome's;
an outcome with no key reads as 0, as Mimir holds no series for an outcome that never happened.
Mimir holds one export when the run starts; the first query after the sweep publishes a second,
sampled a second after now, because the wire carries milliseconds and the scenario waits for a
sample strictly after its sweep, which a sample at now could truncate to before.

A scenario's `uses` line names the graph nodes it needs, then the settings its steps read. storage and
events point at no trace, so they use no grafana up; sweeper reads Mimir through Grafana, so it
does.

## Styling

`style`'s SQL tests spell out the escapes, so each case asserts the bytes a terminal receives
rather than restating SQL through `Style`'s methods.
