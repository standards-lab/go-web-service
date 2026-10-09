# The OpenTelemetry Collector, contrib distribution, receiving the service's
# logs, traces, and metrics and forwarding each to its backend. This FROM line
# is the collector's one pin.
FROM otel/opentelemetry-collector-contrib:0.162.0

COPY otel-collector.yaml /etc/otelcol-contrib/otel-collector.yaml

# No HEALTHCHECK: the contrib image ships no shell, so a probe against the
# collector's own health_check extension has nothing to run it with. The
# exporters retry with backoff, so a backend that is still starting costs
# nothing but delay.

CMD ["--config=/etc/otelcol-contrib/otel-collector.yaml"]
