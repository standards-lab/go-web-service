# Tasks

Each task wraps a plain command, so the repository works without mise, with two caveats:
`mise.toml` sets `APP_ENV=local`, so a bare `go run ./cmd/server` outside mise loads the base
configuration alone and binds `0.0.0.0` at info logging, with no seed set, instead of the local
overlay's loopback, debug, and `default` set — set `APP_ENV=local` yourself when running without
mise. `serve`'s full command also needs a shell that understands `/dev/tcp` (bash, not `sh`); see
[`compose/README.md`](../compose/README.md).

| Task | Command | What it does |
|------|---------|--------------|
| `mise run check` | per module (`.`, `tools/slab`): build, vet, `gofmt -l`, `go fix -diff`, `go mod tidy -diff`, `go test -race`, `golangci-lint`; then `go tool sqlint`, `lint-context`, and `lint-references` | The one read-only check, run by CI; vet, fix, and lint read the integration suite too |
| `mise run currency` | `scripts/currency.sh` | Report every requirement, Go version, tool, action pin, and image tag behind its latest; writes nothing |
| `mise run upgrade` | per module: `go mod edit -go=<current minor> -toolchain=none`, `go get` latest of the direct requirements, `go mod tidy`; then `mise upgrade --bump --local` | Upgrade the go directives, the requirements, and the mise tools; actions and images move by hand from currency's report |
| `mise run vet` | `go vet -tags integration ./...`, then `go vet ./...` in `tools/slab` | Compile-check and vet, the integration suite and slab included |
| `mise run serve` | `go run ./cmd/server`, tee'd to the observability collector | Run the service locally |
| `mise run test` | `go test -race ./...`, then the same in `tools/slab` | Run the unit tier, slab included |
| `mise run integration` | an isolated `docker compose up --build`, `go test -tags integration ./integration/`, the stack's logs on a failure, `down -v` | Run the integration tier against its own stack |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./... && go tool sqlint`, then `golangci-lint run ./...` in `tools/slab` | Lint the Go and the SQL, slab included |
| `mise run db:up` | `docker compose up -d --wait --build` | Start the local Postgres, Azurite, and NATS |
| `mise run db:down` | `docker compose down` | Stop the local Postgres, Azurite, and NATS (keep data) |
| `mise run db:reset` | `docker compose down -v` | Stop the local Postgres, Azurite, and NATS and drop their data |
| `mise run otel:up` | `docker compose ... up -d --wait --build`, then polls Mimir until it can query | Start the collector, Loki, Tempo, Mimir, and Grafana |
| `mise run otel:down` | `docker compose --profile observability down …` | Stop the observability profile (keep data) |
| `mise run otel:reset` | `docker compose --profile observability down -v …` | Stop the observability profile and drop its data |
| `mise run stack:up` | `db:up` and `otel:up` | Start the local Postgres, Azurite, and NATS and the observability profile |
| `mise run stack:down` | `db:down` and `otel:down` | Stop the local Postgres, Azurite, and NATS and the observability profile (keep data) |
| `mise run stack:reset` | `db:reset` and `otel:reset` | Stop the local Postgres, Azurite, and NATS and the observability profile and drop their data |
| `mise run db:state <state>` | `curl -d '{"state":"<state>","confirm":true}' localhost:8080/admin/database/state` | Reset the running service's database to a named state |
| `mise run slab -- scenario <name>` | `cd tools/slab && go run ./cmd/slab` | Run a narrated scenario (`mise run slab -- scenario` lists them) — see [`tools/slab/README.md`](../tools/slab/README.md) |

`check` and `upgrade` run in each module `GO_MODULES` lists in `mise.toml` (`.` and
`tools/slab`); `vet`, `test`, and `lint` name both modules; `tidy` runs in the root module. The
check stops at the first Go or sqlint failure; its `go test` runs the unit tier only, the
integration tier staying behind its build tag, while vet, fix, and lint read the integration suite
too (`-tags integration`). `lint-context` and `lint-references` both run before either fails the
check. slab is its own module so go-cli-sdk, `golang.org/x/term`, and `golang.org/x/sys` never
enter `cmd/server`'s dependency graph; its task runs from `tools/slab` rather than passing through
at the root, so a clone without a `go.work` still resolves slab's dependencies against the
published releases.

Compose's `up` always starts the unprofiled services alongside an activated profile, and its
`down` is project-wide, so `otel:up`, `otel:down`, and `otel:reset` name the five observability
services to leave the `db:` group alone. Neither half of the `stack:` tasks depends on the other,
so mise runs each pair in parallel.

`mise run slab -- scenario storage` narrates the storage end to end against the running service: the
`blobfs` and `app` migration sets, the seeded logos and document tree, the validators and refusals,
and a recursive delete waited out through the sweep. `mise run slab -- scenario events` runs a
command for every event type, for `events watch` ([Adding an event](messaging.md#adding-an-event))
to print.
