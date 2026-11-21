# Compose

The service's local development stack, defined in `compose.yml` at the repository root and the
files under this directory, one file per service group. `compose.yml` names the project
(`go-web-service`) and lists `include:` entries; each included file owns one group's services,
ports, volumes, and, where it applies, its `profiles:` gate.

Every service builds from its own `compose/<service>/Dockerfile`, whose `FROM` line is the
service's one image pin. No `image:` line names an image, in compose or in CI, and
`mise run currency` reads the `FROM` lines. The Dockerfile bakes in the service's configuration,
environment, and command, so compose bind-mounts no configuration. Where the base image can run
a probe (Postgres, Azurite, NATS, Grafana), a `HEALTHCHECK` in the Dockerfile defines readiness. The
collector, Loki, Tempo, and Mimir images ship no shell, so their Dockerfiles have no
`HEALTHCHECK`, and compose waits on them as running. Every `up` passes `--build`, so an edited
Dockerfile or configuration file takes effect on the next start. These are development stacks:
data lives in named volumes, which each group's `down` task keeps and its `reset` task deletes.

## Postgres

`compose/postgres.yml` runs Postgres 19 (`compose/postgres/`), the service's declared SQL engine
(see [Stack](../docs/stack.md)), as user, password, and database `app`. Those are the development
values `config.json` and `secrets.example.json` name, not secrets. Its health check runs
`pg_isready` over TCP, not the socket, because the first start's initialization runs a temporary
server on the socket alone, which would report ready before the real server starts. It carries no
profile, so every compose invocation starts it. `mise run db:up` brings it up and waits for health;
`mise run db:down` stops it; `mise run db:reset` also drops its volume.

## Azurite

`compose/azurite.yml` runs Azurite (`compose/azurite/`), Azure's blob storage emulator, as the
service's local object store (see [Stack](../docs/stack.md)). Like Postgres it carries no profile,
so `mise run db:up` starts both and `db:down` and `db:reset` stop them. Its blob endpoint listens on
`127.0.0.1:10000` (`AZURITE_BLOB_PORT` to change it) under Azurite's well-known development account,
`devstoreaccount1`. `secrets.example.json` carries that account's published key, which is never a
production credential. Azurite runs with `--skipApiVersionCheck`, because the Azure SDK the
service's provider pins sends a service version newer than the image accepts by default. The image
defines no health check and has no curl, so the Dockerfile's health check probes the blob listener
with busybox `nc`. The service creates its container on start. Data persists in the
`go-web-service-azurite` volume.

## NATS

`compose/nats.yml` runs NATS 2 with JetStream (`compose/nats/`), the broker the service's messaging
runtime publishes through (see [Stack](../docs/stack.md)). Like Postgres it carries no profile, so
`mise run db:up` starts it with the others and `db:down` and `db:reset` stop it. It listens on
`127.0.0.1:4222` (`NATS_PORT` to change it), the URL `config.json`'s `nats` block names; the
monitoring endpoint stays inside the container, where the health check waits for JetStream itself
(`/healthz?js-enabled-only=true`), not merely the listener, because the service's broker, started
any earlier, would fail to provision its stream. The probe uses the image's busybox `wget`. The
service owns its stream, `WEB_SERVICE`, and creates it on start. Data persists in the `go-web-service-nats` volume.

## The observability profile

`compose/observability.yml` and the five service directories it builds (`compose/otel-collector/`,
`loki/`, `tempo/`, `mimir/`, and `grafana/`) run an OpenTelemetry Collector alongside a local
Loki, Tempo, Mimir, and Grafana stack (LGTM). All five services carry
`profiles: ["observability"]`, so no ordinary compose invocation starts them: `mise run db:up` and
the `integration` task both omit `--profile` and stay unaffected. `mise run otel:up` brings the
profile up and waits for health; `mise run otel:down` stops it, naming the five services so it
does not also stop Postgres (`docker compose down` is project-wide otherwise). Data persists in
named volumes (`go-web-service-loki`, `-tempo`, `-mimir`, `-grafana`) across a plain `down`;
`mise run otel:reset` also drops them.

The collector, Loki, Tempo, and Mimir have no `HEALTHCHECK`, so their dependents wait on
`service_started`. None of their images has a `wget` or a shell to probe with: the collector's
contrib image ships no shell, Loki's has been only `/usr/bin/loki` since 3.5.8 and Tempo's only
`/tempo` since 2.10 (busybox was removed from both), and Mimir's is distroless (only
`/bin/mimir`). The collector's exporters retry with backoff, so a backend that is still starting
costs nothing but delay. Grafana's health check asks its `/api/health` endpoint, which answers
only once Grafana's database is reachable.

Mimir's HTTP endpoint answers ready a few seconds before its querier ring finishes joining — a
query run in that window fails with `500` ("could not get all queriers from the ring: empty
ring"). Compose's own `--wait` cannot see this, since it only watches the container health check
Mimir's image cannot run (above), so `otel:up` polls a synthetic query
(`api/v1/query?query=vector(1)`, no series to read, just the query path itself) until Mimir
actually answers, and only then returns. `mimir.yaml`'s single-binary settings (one ingester, a
local in-memory ring, and `min_ready_duration: 0s`, since the default 15s exists to pace rolling
updates) keep that window to a couple of seconds.

The service itself, `go-web-service`, runs on the host through `mise run serve`
(`go run ./cmd/server`), not as a compose service — it has not been fitted into a container
image. Everything it sends to the stack crosses the loopback ports the collector publishes.

### How a signal reaches its backend

Three independent paths run through the collector (`compose/otel-collector/otel-collector.yaml`),
one per signal, each ending in a different backend.

**Logs.** `mise run serve` streams its stdout, one JSON line per `slog` record, over a TCP
connection to the collector's `tcp_log` receiver (port 4319 by default, `OTEL_LOG_PORT` to change
it) — the mechanism is below. The receiver's `json_parser` operator reads each line's keys into
attributes, then promotes the standard ones onto the record itself: `time` becomes the record's
timestamp, `level` its severity, and `trace_id`/`span_id` — hex strings `go-observability`'s
`NewTraceHandler` stamps on every record whose context carries a live span — become the record's own
trace and span id fields, OpenTelemetry's native home for them rather than another attribute. `msg`
becomes the record body; the promoted fields are then removed from attributes so each reaches Loki
exactly once. Every record also carries `resource.service.name = go-web-service`, since the receiver
has no resource of its own. That value must match what the service's own telemetry reports, so Loki
indexes the logs under the same `service_name` Tempo's trace-to-logs link searches by. From there
the `otlp_http` exporter posts to Loki's native OTLP endpoint (`http://loki:3100/otlp`; the exporter
appends `/v1/logs` itself). Loki turns the resource's `service.name` into the indexed stream label
`service_name`, and the record's own trace and span ids and any remaining attributes into structured
metadata attached to each line.

With no `multiline` block the `tcp_log` receiver splits the byte stream at newlines
(`pkg/stanza/split`'s `NewlineSplitFunc`), trims surrounding whitespace, and flushes an
unterminated final line when the connection closes (the tcp input builds its split function with
`flushAtEOF=true`). slog ends every record with `\n`, so the defaults frame it correctly, and a
write that batches several lines into one TCP segment is fine because `one_log_per_packet` stays
false. A line without `trace_id` and `span_id` (any record outside a request) parses cleanly with
those fields unset. The `move` and `remove` operators fail on a missing field, so each is guarded
by an `if`: a line the parser rejected has none of those attributes.

**Traces.** The collector's `otlp` receiver listens on 4317 (grpc) and 4318 (http), bound to all
interfaces because the default, localhost, is not reachable through the container's published ports;
the service's tracing middleware starts a span on every request and the `otlp` sub-module's gRPC
exporter sends it here, batched, on the service's own shutdown-bounded flush. From there the
`otlp_grpc` exporter forwards to Tempo's own OTLP receiver (`tempo:4317`, a different container's
port, not the collector's). The hop is plaintext inside the compose network, and `tempo.yaml` binds
Tempo's receivers to all interfaces, since Tempo 3 binds them to localhost by default. Tempo 3
batches incoming spans directly through its live-store (no separate ingester), cutting blocks every
30 seconds by default and retaining them 24 hours (`compose/tempo/tempo.yaml`), on local filesystem
storage. The live-store's defaults stand unchanged (it holds blocks 20 minutes); the 24-hour block
retention is a compaction setting, and since the single-binary target runs both the scheduler that
plans compaction jobs and the worker that executes them, `tempo.yaml` sets it in each one's own
copy.

**Metrics.** Two sources feed this pipeline: the same `otlp` receiver, now carrying the service's
own metrics (otelhttp's request-duration histogram, exported through a periodic reader), and a
`prometheus/self` scrape of the collector's own internal telemetry every 15 seconds — added so
the pipeline, and the dashboard below, have a real signal independent of the service. The
collector serves that telemetry on `localhost:8888`, loopback only, since it never needs to leave
the container. The `prometheus_remote_write` exporter pushes to Mimir (`http://mimir:9009/api/v1/push`), which
ingests through its single-binary target (`-target=all`) and stores blocks on local filesystem
storage.

### Streaming logs from `mise run serve`

The `serve` task (`mise.toml`) opens a TCP connection to the collector's log port before it
builds anything, and exits immediately if nothing answers — `mise run otel:up` has to run first.
It then runs the service with its stdout `tee`'d to that connection and, through a duplicated
file descriptor, to the terminal, so a developer still sees their own logs directly. A socket
cannot be opened by path, so the connection takes `tee`'s stdout through a duplicate, and the
terminal, saved on file descriptor 4, is `tee`'s path argument (`/dev/fd/4`). The collector is a
hard dependency, started before the service, with no graceful fallback. `tee -i`
ignores Ctrl-C's SIGINT so it outlives the signal and only exits once the server's own graceful
shutdown closes its stdout at ordinary EOF, rather than dying mid-drain and losing the server's
last log line. If the collector goes away while `serve` is running — a restart of the
observability profile, most commonly — the connection breaks, and Go's runtime raises `SIGPIPE`
on the next write to the now broken pipe, killing the service outright. This is a deliberate,
accepted limitation, not an oversight: restart the observability profile only while `serve` is
stopped. A service-owned
configurable log destination was considered as the fix and rejected: a socket the service writes
to directly loses a line whenever that socket is down, with none of OTLP's batching or retry, for
a failure confined to local development and scheduled to disappear once `v1.deployment`
containerizes the service and its own runtime captures stdout.

`log.format` is `json`, so the `json_parser` operator reads every line's structured fields —
`trace_id` and `span_id` included.

### Correlation in Grafana

`compose/grafana/provisioning/` holds Grafana's provisioning tree, copied by
`compose/grafana/Dockerfile` onto the image's own provisioning path so the on-disk layout matches
what Grafana reads. (The image sets `GF_PATHS_PROVISIONING=/etc/grafana/provisioning`, and Grafana
reads `datasources/` and `dashboards/` beneath it at startup.) `datasources/datasources.yaml`
declares the three backends, cross-linked by explicit uid, which keeps Grafana from generating
opaque ones the cross-links could not name:

- Loki's derived field opens a log's trace in Tempo. It matches on the structured-metadata label
  `trace_id` directly, not a regex over the log line, because the log line's visible text is only
  the message — the trace id lives in structured metadata, per the logs path above. Loki's OTLP
  ingestion stores the record's TraceId as structured metadata named `trace_id`, and Grafana merges
  structured metadata into a row's labels, so `matcherType: label` tests `matcherRegex` against the
  label keys. For this internal link the URL is the Tempo query itself: the trace id.
- Tempo's `tracesToLogsV2` and `tracesToMetrics` open a trace's correlated logs (matched on
  `trace_id`) and correlated metrics (matched on `service.name`, which the metrics exporter maps to
  Mimir's `job` label). `tracesToLogsV2` maps the resource attribute `service.name` to the Loki
  stream label `service_name`, and with `filterByTraceID` Grafana 13 appends a `label_format` stage
  that matches either a line containing the id or the structured-metadata `trace_id`, with no custom
  query. `tracesToMetrics`' query names the OpenTelemetry HTTP server duration histogram,
  `http_server_request_duration_seconds`.
- Mimir's `exemplarTraceIdDestinations` will open a metric's exemplar trace in Tempo once something
  exports exemplars; Mimir drops them today (`max_global_exemplars_per_user` is unset, defaulting to
  0), a one-line gap for whichever later step first needs them. Exemplars arrive with the trace id
  as a `trace_id` exemplar label (the `prometheusremotewrite` exporter's name for the OTLP
  exemplar's TraceId), and the SDK's default exemplar filter attaches one to any measurement
  recorded inside a sampled span, so the service side needs no configuration.
- Mimir serves the Prometheus query API under `/prometheus` (`-http.prometheus-http-prefix`), which
  Grafana's Prometheus datasource type speaks directly. `prometheusVersion: 2.9.1` is the highest
  value Grafana's Mimir list offers ("> 2.9.x") and selects the modern label-matching endpoints
  Mimir 3 serves; `timeInterval` is the collector's 15-second self-scrape, the metrics source the
  stack always has.

A literal `$` in `datasources.yaml` is written `$$`: Grafana's provisioning reader expands `$VAR`
from the environment, and `$$` escapes the template variables Grafana itself interpolates at link
time.

`dashboards/observability-stack.json` is the first dashboard, provisioned read-only
(`allowUiUpdates: false` in `dashboards/dashboards.yaml`, so the file stays the source of truth).
Its Collector and Pipelines rows read the collector's own self-telemetry; its Backends row queries
the service's own logs and traces. Grafana reads the provider file once at startup but re-reads
the JSON files it points at every `updateIntervalSeconds` (30), so an edited dashboard takes
effect without a restart. The provider's path is the provisioned `dashboards/` directory, which also holds the
provider file; Grafana treats only `*.json` there as dashboards, so the provider file is not
mistaken for one.

### Ports

Every port below binds `127.0.0.1` only; the environment variable overrides the default. The log
port, 4319, sits beside the OTLP pair so the collector's three ingest ports read as one block.

| Port | Variable | Service | Carries |
|---|---|---|---|
| 4317 | `OTEL_GRPC_PORT` | collector | OTLP grpc (traces, metrics) |
| 4318 | `OTEL_HTTP_PORT` | collector | OTLP http |
| 4319 | `OTEL_LOG_PORT` | collector | the service's stdout log stream |
| 13133 | `OTEL_HEALTH_PORT` | collector | the `health_check` extension; no `HEALTHCHECK` consumes it (the image has no shell), but the host can curl it |
| 3100 | `LOKI_PORT` | loki | its API |
| 3200 | `TEMPO_PORT` | tempo | its query API |
| 9009 | `MIMIR_PORT` | mimir | its Prometheus-compatible query API and remote-write ingestion |
| 3000 | `GRAFANA_PORT` | grafana | the UI (anonymous admin access, no login) |

### File layout

```
compose.yml                                 the project name and the included group files
compose/postgres.yml, azurite.yml, nats.yml the database, object store, and broker, no profile
compose/observability.yml                   the five observability services, the profile gate
compose/<service>/Dockerfile                each service's image pin, configuration, and health check
compose/otel-collector/otel-collector.yaml  receivers, processors, exporters, pipelines
compose/loki/, tempo/, mimir/               each backend's own single-binary, local-storage config
compose/grafana/provisioning/datasources/…  the three datasources and their cross-links
compose/grafana/provisioning/dashboards/…   the dashboard provider and observability-stack.json
```
