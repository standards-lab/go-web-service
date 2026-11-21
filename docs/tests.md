# Tests

Two tiers. The unit tier, `mise run test`, runs on every pull request inside `mise run check`
and touches no service, network, or disk: a package that runs SQL proves it over sqlate's
scripted driver, and one that stores objects over go-storage's fake. The integration tier,
`mise run integration`, runs the composed service black-box through its API against the compose
stack, in CI on every merge to main and on demand from the Actions tab.

The suite lives in the `integration` package under the `integration` build tag. Each file is
named for the surface it asserts (`organization_test.go`, `document_test.go`, `logo_test.go`,
`sweep_test.go`, `outage_test.go`, and the rest), and each test's comment states what it proves.
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

The task runs the same `compose.yml` as its own project (`go-web-service-integration`, Postgres
on 5433, Azurite on 10001, and NATS on 4223), so the development stack and its data are never touched, and
tears the stack down with its volumes when the suite ends, so every run starts from an empty
database and container. A failing run prints the stack's logs before the teardown; CI's
`integration` job runs the task as it is. The harness honors `APP_DATABASE_HOST`,
`APP_DATABASE_PORT`, `APP_DATABASE_PASSWORD`, `APP_STORAGE_ENDPOINT`, `APP_STORAGE_ACCOUNT`,
`APP_STORAGE_KEY`, and `APP_NATS_URL` for a stack elsewhere.
