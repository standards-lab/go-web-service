# Tempo, the observability profile's trace backend.
# See compose/README.md
FROM grafana/tempo:3.1.1

COPY tempo.yaml /etc/tempo/tempo.yaml

CMD ["-config.file=/etc/tempo/tempo.yaml"]
