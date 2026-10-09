# Loki, the log backend, as a single binary on local storage. This FROM line is
# Loki's one pin.
FROM grafana/loki:3.7.8

COPY loki.yaml /etc/loki/loki.yaml

# No HEALTHCHECK: since 3.5.8 the image is only /usr/bin/loki (busybox was
# removed), so there is no wget or shell to probe /ready with. Dependents wait
# on service_started.

CMD ["-config.file=/etc/loki/loki.yaml"]
