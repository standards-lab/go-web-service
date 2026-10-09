# Mimir, the metrics backend, as a single binary (-target=all) on local
# storage. This FROM line is Mimir's one pin.
FROM grafana/mimir:3.2.2

COPY mimir.yaml /etc/mimir/mimir.yaml

# No HEALTHCHECK: the Mimir image is distroless (only /bin/mimir), so there is
# no wget or shell to probe /ready with. Dependents wait on service_started;
# mimir.yaml's single-binary ring settings make it ready within a couple of
# seconds of that, and `mise run otel:up` polls a query until it answers.

CMD ["-config.file=/etc/mimir/mimir.yaml", "-target=all"]
