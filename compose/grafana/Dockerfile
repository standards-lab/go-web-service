# Grafana, over the three backends, with anonymous admin access so the local
# stack needs no login. This FROM line is Grafana's one pin.
FROM grafana/grafana:13.2.3

ENV GF_AUTH_ANONYMOUS_ENABLED=true \
    GF_AUTH_ANONYMOUS_ORG_ROLE=Admin

# The image sets GF_PATHS_PROVISIONING=/etc/grafana/provisioning; Grafana reads
# datasources/ and dashboards/ beneath it at startup.
COPY provisioning /etc/grafana/provisioning

HEALTHCHECK --start-period=60s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:3000/api/health"]
