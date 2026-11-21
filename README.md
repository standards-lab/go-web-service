# go-web-service

The reference web service of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), a
minimal-dependency Go implementation of
[Elemental Architecture](https://github.com/standards-lab/architecture/blob/main/architecture.md).
Initialized from the [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template).

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks. The database password
and the object store's key live in the gitignored secrets layer; copy the committed example to
create it. A missing `secrets.json` is not a load error: the configuration loads without them,
and the service then exits at construction with `storage: azureblob: storage key required`, so
this step comes first:

```sh
mise trust && mise install
cp secrets.example.json secrets.json

mise run db:up      # start the local Postgres, Azurite, and NATS and wait for health
mise run otel:up    # start the observability collector and stack
mise run serve      # run the service
```

`serve` streams its stdout to the collector over TCP and exits immediately if `otel:up` has not
run first — see [`compose/README.md`](compose/README.md) for the mechanism and its one accepted
limitation.

The service is one dependency graph, described in `internal/app`, and `App.Run` runs it as a
go-web-sdk `web.Service`, starting it in layer order. Telemetry starts first, so its providers
are installed before anything they record. The database, the object store, and the NATS broker
connect next, together in one layer. The schema
starts after them: it applies any pending migration, checks every statement against the schema,
and seeds the configured state (the `local` overlay names `default`). The [sweep](docs/sweep.md) and
the [messaging](docs/messaging.md) relay start after the schema, and the server starts last, alone in
the top layer. The drain runs in reverse: the server drains first, so in-flight requests
complete before what they run on closes; the relay then makes its last pass, publishing what
those requests committed, before the broker and the database close; and telemetry flushes
last. The service then logs `server ready` on `localhost:8080` (the `local` overlay binds
loopback and runs debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}  — lifecycle, database, storage, broker, schema, sweeper, relay
```

Ctrl-C drains in-flight requests and exits with `server stopped`. A failure to start, run, or
drain logs `service failed` with the error, which names the failing node, and exits 1. A
constructor's failure goes to stderr instead, as `service failed: <error>`, since the logger may
be what failed. That serve, probe, and drain sequence is the one check a change to the
composition root is verified by hand with; everything the running service does is asserted by
the integration tier ([`docs/tests.md`](docs/tests.md)).

### Adding a service

A service is a node in the graph. Define it in its layer's define function, in that layer's file
in `internal/app` (`defineDomain` in `domain.go` for a domain service, for example), and add its
handle to `Nodes`. Its part in the lifecycle is inferred from its value's methods: a `Start` or
`Shutdown` makes it a participant, a `Ready` on a value that also has `Start` adds its check to
`/readyz` (a `Ready` alone adds none), and an `Err` channel has its runtime failure watched, so
nothing registers it. Its place in the order follows from the nodes it uses, plus any
`graph.After` on its Define line, which orders it after a node it does not use. A reactor's node
joins the reactors group, `Nodes.Reactors`, with `graph.In` on its Define line. The group is one
of `App.Roots`, the root set `Run` builds, and the server orders itself after it. A node that
commits events outside a request, such as a consuming reactor, Uses the relay's node, so it
drains before the relay's last pass; a request-driven producer, as every domain service is, needs
no edge, since the server drains first.

## Documentation

The pages under `docs/` hold the detail:

- [Stack](docs/stack.md): the declared stack, the standard and native tiers, and what a provider
  swap changes.
- [API](docs/api.md): the routes mounted under `/api`.
- [Admin](docs/admin.md): the database admin service under `/admin/database`.
- [Sweep](docs/sweep.md): go-database's general sweeper and its three reclaimers.
- [Messaging](docs/messaging.md): the event convention, the event catalog, the file protocols'
  hooks, and adding an event.
- [Tasks](docs/tasks.md): the mise tasks.
- [Tests](docs/tests.md): the unit and integration tiers.
- [Configuration](docs/configuration.md): the configuration layers, their precedence, and their
  keys.
- [Domain architecture](docs/domain-architecture.md): the reasoning behind the domain layers.

## License

Apache 2.0, see [LICENSE](LICENSE).
