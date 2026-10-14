# The development stack's NATS: the JetStream server the service's broker
# publishes to, which config.json's nats block names. This FROM line is
# NATS's one pin.
FROM nats:2.15.1-alpine

# JetStream on, its store in /data, the stack's named volume, and the
# monitoring endpoint on 8222 for the health check.
CMD ["-js", "-sd", "/data", "-m", "8222"]

# Healthy once JetStream is enabled, not merely once the server listens: a
# broker that starts earlier would fail to provision its stream. The probe
# uses the image's busybox wget.
HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8222/healthz?js-enabled-only=true"]
