# Tempo, the trace backend, as a single binary on local storage. This FROM line
# is Tempo's one pin.
FROM grafana/tempo:3.1.0

COPY tempo.yaml /etc/tempo/tempo.yaml

# No HEALTHCHECK: since 2.10 the image is only /tempo (busybox was removed), so
# there is no wget or shell to probe /ready with. Dependents wait on
# service_started.

CMD ["-config.file=/etc/tempo/tempo.yaml"]
