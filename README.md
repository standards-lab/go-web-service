# Service

Generated from [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## After generation

Three steps localize the service's identity:

1. Rename `envPrefix` in `internal/config/config.go` — the single constant every `APP_*`
   environment-variable name derives from.
2. Rename the `APP_ENV` key in `mise.toml`'s `[env]` block to follow the prefix.
3. Rewrite this README for the service.

Licensing and release automation are yours to define; CI arrives working (`.github/workflows/ci.yml`).

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks:

```sh
mise trust && mise install
mise run serve
```

The service logs `server ready` on `localhost:8080` (the `local` overlay binds loopback and runs
debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}
```

Ctrl-C drains in-flight requests and exits with `server stopped`.

## Tasks

Each task wraps a plain command, so the repository works without mise:

| Task | Command | What it does |
|------|---------|--------------|
| `mise run vet` | `go vet ./...` | Compile-check and vet |
| `mise run serve` | `go run ./cmd/server` | Run the service locally |
| `mise run test` | `go test -race ./...` | Run the tests |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run ./...` | Lint |

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json` — the base file, every knob at its default.
2. `config.<APP_ENV>.json` — the environment overlay; `mise.toml` sets `APP_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging).
3. `secrets.json`, `secrets.<APP_ENV>.json` — gitignored secret layers.
4. `APP_*` environment variables — the final override: `APP_LOG_LEVEL`, `APP_LOG_FORMAT`,
   `APP_SERVER_HOST`, `APP_SERVER_PORT`, the four server timeout variables, and
   `APP_SHUTDOWN_TIMEOUT`.

Every file is optional — a deployment can run on the base file and environment variables alone,
or on environment variables only.

## Building out the service

The build points live in `internal`: `infrastructure/infrastructure.go` for the services the
application composes on, `app/routes.go` for its domain services, `app/middleware.go` for its
router-level middleware. `cmd/server` is the entrypoint alone and never changes.

A domain service starts from its Entity. Give the Entity its own package, expose its Queries
and Commands as the domain service's methods, bind those methods to routes in a `web.Module`,
and mount the module in `routes` (`internal/app/routes.go`), its constructor drawing what it
uses from the `Infrastructure` fields.

An infrastructure service (a database pool, a storage client, an auth client) is a field on
`Infrastructure` plus its construction in `New` (`internal/infrastructure/infrastructure.go`):
assign the field, then declare the lifecycle on the coordinator —

```go
i.Pool = pool
lc.Add(lifecycle.Service{Name: "db", Stage: 0, Start: pool.Ping, Shutdown: pool.Close, Check: pool})
```

Numbered stages start in ascending order ahead of the server's root stage and drain after it,
so in-flight requests complete before their infrastructure closes. A service declared this way
cannot be missing from the probe or the drain, and a field that does not exist fails the build
at its access.

Middleware that applies to every route stacks in `middleware` (`internal/app/middleware.go`),
outermost first; middleware scoped to one domain service belongs on its module.

Configuration grows by adding fields to `Config` in `internal/config/config.go` and delegating
to their `Merge` and `Finalize` in the existing shape. The service keeps pace with its SDKs by
updating its `go-core` and `go-web-sdk` pins and applying whatever adjustments the release
notes call for.
