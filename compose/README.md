# Compose

The service's local development stack, defined in `compose.yml` at the repository root and the
files under this directory, one file per service group. `compose.yml` only lists `include:`
entries; each included file owns one group's services, environment, volumes, and, where it
applies, its `profiles:` gate.

## Postgres

`compose/postgres.yml` runs Postgres 18, the service's declared SQL engine (the README's [Stack](../README.md#stack) section).
It carries no profile, so every compose invocation starts it. `mise run db-up` brings it up and
waits for health; `mise run db-down` stops it; `mise run db-reset` also drops its volume.

## The observability profile

`compose/observability.yml` and the files under `compose/observability/` run an OpenTelemetry
Collector alongside a local Loki, Tempo, Mimir, and Grafana stack (LGTM). All five services carry
`profiles: ["observability"]`, so no ordinary compose invocation starts them: `mise run db-up` and
the `integration` task both omit `--profile` and stay unaffected. `mise run otel-up` brings the
profile up and waits for health; `mise run otel-down` stops it, naming the five services so it
does not also stop Postgres (`docker compose down` is project-wide otherwise). Data persists in
named volumes (`go-web-service-loki`, `-tempo`, `-mimir`, `-grafana`) across a plain `down`;
`mise run otel-reset` also drops them.

Mimir's HTTP endpoint answers ready a few seconds before its querier ring finishes joining — a
query run in that window fails with `500` ("could not get all queriers from the ring: empty
ring"). Compose's own `--wait` cannot see this, since it only watches the container healthcheck
Mimir does not have (below), so `otel-up` polls a synthetic query
(`api/v1/query?query=vector(1)`, no series to read, just the query path itself) until Mimir
actually answers, and only then returns.

The service itself, `go-web-service`, runs on the host through `mise run serve`
(`go run ./cmd/server`), not as a compose service — it has not been fitted into a container
image. Everything it sends to the stack crosses the loopback ports the collector publishes.

### How a signal reaches its backend

Three independent paths run through the collector (`compose/observability/otel-collector.yaml`),
one per signal, each ending in a different backend.

**Logs.** `mise run serve` streams its stdout, one JSON line per `slog` record, over a TCP
connection to the collector's `tcp_log` receiver (port 4319 by default, `OTEL_LOG_PORT` to change
it) — the mechanism is below. The receiver's `json_parser` operator reads each line's keys into
attributes, then promotes the standard ones onto the record itself: `time` becomes the record's
timestamp, `level` its severity, and `trace_id`/`span_id` — hex strings `go-observability`'s
`NewTraceHandler` stamps on every record whose context carries a live span — become the record's
own trace and span id fields, OpenTelemetry's native home for them rather than another attribute.
`msg` becomes the record body; the promoted fields are then removed from attributes so each
reaches Loki exactly once. Every record also carries `resource.service.name = go-web-service`,
since the receiver has no resource of its own. From there the `otlp_http` exporter posts to
Loki's native OTLP endpoint. Loki turns the resource's `service.name` into the indexed stream
label `service_name`, and the record's own trace and span ids and any remaining attributes into
structured metadata attached to each line.

**Traces.** The collector's `otlp` receiver listens on 4317 (grpc) and 4318 (http); the service's
tracing middleware starts a span on every request and the `otlp` sub-module's gRPC exporter sends
it here, batched, on the service's own shutdown-bounded flush. From there the `otlp_grpc`
exporter forwards to Tempo's own OTLP receiver (`tempo:4317`, a different container's port, not
the collector's). Tempo 3 batches incoming spans directly through its live-store (no separate
ingester), cutting blocks every 30 seconds by default and retaining them 24 hours
(`compose/observability/tempo.yaml`), on local filesystem storage.

**Metrics.** Two sources feed this pipeline: the same `otlp` receiver, now carrying the service's
own metrics (otelhttp's request-duration histogram, exported through a periodic reader), and a
`prometheus/self` scrape of the collector's own internal telemetry every 15 seconds — added so
the pipeline, and the dashboard below, have a real signal independent of the service. The
`prometheus_remote_write` exporter pushes to Mimir (`http://mimir:9009/api/v1/push`), which
ingests through its single-binary target (`-target=all`) and stores blocks on local filesystem
storage.

### Streaming logs from `mise run serve`

The `serve` task (`mise.toml`) opens a TCP connection to the collector's log port before it
builds anything, and exits immediately if nothing answers — `mise run otel-up` has to run first.
It then runs the service with its stdout `tee`'d to that connection and, through a duplicated
file descriptor, to the terminal, so a developer still sees their own logs directly. `tee -i`
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
`trace_id` and `span_id` included, once the service is instrumented enough to have one live.

### Correlation in Grafana

`compose/observability/grafana/provisioning/` holds Grafana's provisioning tree, mounted
directly onto the container's own provisioning path so the on-disk layout matches what Grafana
reads. `datasources/datasources.yaml` declares the three backends, cross-linked by explicit uid:

- Loki's derived field opens a log's trace in Tempo. It matches on the structured-metadata label
  `trace_id` directly, not a regex over the log line, because the log line's visible text is only
  the message — the trace id lives in structured metadata, per the logs path above.
- Tempo's `tracesToLogsV2` and `tracesToMetrics` open a trace's correlated logs (matched on
  `trace_id`) and correlated metrics (matched on `service.name`, which the metrics exporter maps
  to Mimir's `job` label).
- Mimir's `exemplarTraceIdDestinations` will open a metric's exemplar trace in Tempo once
  something exports exemplars; Mimir drops them today (`max_global_exemplars_per_user` is unset,
  defaulting to 0), a one-line gap for whichever later step first needs them.

`dashboards/observability-stack.json` is the first dashboard, provisioned read-only
(`allowUiUpdates: false` in `dashboards/dashboards.yaml`, so the file stays the source of truth).
Its Collector and Pipelines rows read the collector's own self-telemetry, the only live signal
before the service is instrumented; its Backends row queries the service's own logs and traces
and stays empty until then.

### Ports

Every port below binds `127.0.0.1` only; the environment variable overrides the default.

| Port | Variable | Service | Carries |
|---|---|---|---|
| 4317 | `OTEL_GRPC_PORT` | collector | OTLP grpc (traces, metrics) |
| 4318 | `OTEL_HTTP_PORT` | collector | OTLP http |
| 4319 | `OTEL_LOG_PORT` | collector | the service's stdout log stream |
| 13133 | `OTEL_HEALTH_PORT` | collector | the `health_check` extension; no compose healthcheck consumes it (the image has no shell), but the host can curl it |
| 3100 | `LOKI_PORT` | loki | its API |
| 3200 | `TEMPO_PORT` | tempo | its query API |
| 9009 | `MIMIR_PORT` | mimir | its Prometheus-compatible query API and remote-write ingestion |
| 3000 | `GRAFANA_PORT` | grafana | the UI (anonymous admin access, no login) |

### File layout

```
compose/observability.yml                  the five service definitions, the profile gate
compose/observability/
  loki.yaml / tempo.yaml / mimir.yaml      each backend's own single-binary, local-storage config
  otel-collector.yaml                      receivers, processors, exporters, pipelines
  grafana/provisioning/datasources/…       the three datasources and their cross-links
  grafana/provisioning/dashboards/…        the dashboard provider and observability-stack.json
```
