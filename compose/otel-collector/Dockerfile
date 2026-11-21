# The OpenTelemetry Collector, contrib distribution, for the observability profile.
# See compose/README.md
FROM otel/opentelemetry-collector-contrib:0.165.0

COPY otel-collector.yaml /etc/otelcol-contrib/otel-collector.yaml

CMD ["--config=/etc/otelcol-contrib/otel-collector.yaml"]
