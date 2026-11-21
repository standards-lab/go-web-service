# Tests

Tests are black-box: each lives in its package's `<pkg>_test` package and drives the exported
API. golangci-lint's `testpackage` enforces it; the one white-box file allowed is
`export_test.go`, which only exports a clock or probe hook the API cannot reach.

Two tiers. The unit tier, `mise run test`, runs on every pull request inside `mise run check`
and touches no service, network, or disk: a package that runs SQL proves it over sqlate's
scripted driver, and one that stores objects over go-storage's fake. The integration tier,
`mise run integration`, runs the composed service black-box through its API against the compose
stack, in CI on every merge to main and on demand from the Actions tab, below the unit tier's
per-pull-request rate by design; a release is cut only from a main whose run has passed.

The unit tier proves the composition root without a live engine. `internal/app/graph_test.go`
builds the graph without starting it and checks what the root owns: each node named by its
`Nodes` field, a Build of `App.Roots` building exactly what a real Run builds (compared by node
handle), the server alone in the top layer, the reactors group's members, the inferred readiness
checks in their order, telemetry beneath every participant, the connections in one layer below
the schema and the schema below the reactors, the root set dropping no order, and each
participant held by one node. Each placement guard is a function returning an error that carries
the System's layer table; a test runs it against the App's own roots, and a second case runs it
against a fixture that breaks it, so the guard is shown to fail. A test that needs a database
that never answers, or that refuses every connection, listens on a loopback port that accepts
and stays silent, or closes each connection, in place of the engine.

A domain layer's unit tests script the database's answers in the order the protocol asks for
them. Each scripted answer is a helper named for the step it answers (`pendingWrite`,
`deleteBegun`, `treeLocked`, `purged`), and each command's script is a named slice; a test's
name states the rule it proves, and its subtests name the cases.

The suite lives in the `integration` package under the `integration` build tag. Each file is
named for the surface it asserts (`organization_test.go`, `document_test.go`, `logo_test.go`,
`sweep_test.go`, `outage_test.go`, and the rest), and each test's and subtest's name states what it proves.
Its harness is the toolkit the SDKs ship beside what it exercises: go-core's
`process/processtest` builds `cmd/server` once, runs it as a subprocess configured by `APP_*`
variables on a reserved port, and relays the database, the object store, or the broker through
a loopback forwarder (`Options.Database`, `Options.Storage`, `Options.NATS`) that a test severs
for an outage. go-web-sdk's `webtest` drives the service through its API; beneath it,
`integration.Objects` reads the container, `integration.Database` opens a session on the
database, and `integration.Subscribe` reads the service's stream ([Adding an
event](messaging.md#adding-an-event)). `sweep_metrics_test.go` points the service's OTLP exporter at a
gRPC receiver in the test and reads the sweep's metrics from what it exports. The unit tier's
`internal/app/sweep_metrics_test.go` proves each outcome, the backlog gauge per reclaimer, and
the `"sweep pass failed"` record, reporting passes to the sweep metrics node's hook under a
manual reader through `internal/apptest`'s fixtures. A failing test prints each service process's
output, so a 500 comes with the service's own record of it. The service adds only its configuration and state control through
the admin mount; nothing in the service exists for the tests' sake.

Tests that assert what the stream delivers bind a subscriber after the outbox drains, then
commit a marker (an upload) or a probe (an event committed through the outbox, beneath the API)
and wait for it. The relay publishes in commit order, so every event committed before the
marker has arrived by then, and an operation that should deliver nothing is proved by the marker
arriving alone. A test severs a dependency's forwarder to hold an event pending or to have a
pass refused. It shortens the relay's timeout (`APP_MESSAGING_RELAY_TIMEOUT=1s`) and poll
(`APP_MESSAGING_RELAY_POLL=5ms`) so that a relay not held off by the quiesce gate would claim
rows while a reset runs, and lengthens the sweep interval (`APP_SWEEP_INTERVAL=1h`) so that
every pass after the startup cycle is a delete's nudge. The sweep metrics test asserts no
backlog of 1: no reclaimer stays backlogged between collections, so catching one would race an
export against a pass. In the replica claim test a refused claim reports nothing, so the second
sweeper also runs a probe reclaimer listed after `files`, and each probe pass shows a files cycle
was attempted.

The document, sweep, and storage cases work in `finance`, an organization the default state
seeds no document tree for, so each case starts without a root; the seeded tree is acme's,
which `TestSeededStorage` reads, and acme's seeded logo sits under a fixed key every reset
rewrites. A case that asserts a deleting branch severs the object store, so the sweep the delete
nudges is refused each file's object and the branch stays marked. A refused upload is abandoned
and its abandon refused too, leaving the row deleting and hidden yet holding its name until the
sweep's stale reclaim, which those cases run on a 200ms interval and a one-second stale age;
with the store severed, the provider's retries and the abandon outlast the harness client's
failsafe, so such a request uses a client of its own. `hold` sends an upload with
`Expect: 100-continue` and part of its body, so the request is in its handler and the write's
first transaction has committed the pending row. The transfer case resets on a service at the
default timeouts, since reverting and reseeding is no one-second request, and its paced client
writes one 512-byte chunk at a time so each reaches the server when sent. The correlation case
asserts one log record whose `request_id` and `trace_id` both equal the problem's id, which
proves the whole chain (tracing middleware, RequestID, the SDK's problem document, the
correlating handler) where two presence checks would not. Two concurrent starters both reach
ready because the migrator serializes under the schema lock and the seed is idempotent under the
unique constraint.

The task runs the same `compose.yml` as its own project (`go-web-service-integration`, Postgres
on 5433, Azurite on 10001, and NATS on 4223), so the development stack and its data are never touched, and
tears the stack down with its volumes when the suite ends, so every run starts from an empty
database and container. A failing run prints the stack's logs before the teardown; CI's
`integration` job runs the task as it is. The harness honors `APP_DATABASE_HOST`,
`APP_DATABASE_PORT`, `APP_DATABASE_PASSWORD`, `APP_STORAGE_ENDPOINT`, `APP_STORAGE_ACCOUNT`,
`APP_STORAGE_KEY`, and `APP_NATS_URL` for a stack elsewhere.
