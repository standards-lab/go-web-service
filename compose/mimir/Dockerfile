# Mimir, the observability profile's metrics backend.
# See compose/README.md
FROM grafana/mimir:3.2.6

COPY mimir.yaml /etc/mimir/mimir.yaml

CMD ["-config.file=/etc/mimir/mimir.yaml", "-target=all"]
