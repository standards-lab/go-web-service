# Service-owned log destination

Local dev logging today is a shell trick, not a service concern: `mise run serve` (`mise.toml`)
redirects the service's stdout to the observability collector's TCP log port because
`go-web-service` runs as a bare host process and has no other socket to offer
(`compose/README.md`). The redirect makes the collector a hard dependency for the whole run, not
just at startup — Go's runtime raises SIGPIPE and kills the process outright if the collector
goes away mid-run.

The better shape: the service owns its own log destination, selected by configuration
(`internal/config`'s layering, `go-core`'s `logging.Config`), defaulting to stdout alone. Local
dev could then point it at the collector without a shell redirect reaching in from outside; a
real deployment leaves it at the default and lets its own platform capture stdout. Whether this
is worth building is still open — it may be dead weight once the service is containerized, the
way a container runtime's own log capture can make a service-owned socket redundant. That
tension is the same operational-risk shape `standards-lab/context/design/observability-strategy.md`
§4 already reasoned through for the OTLP-logs question (a live network connection is a worse
guarantee than the runtime already capturing stdout); deciding this should cite that reasoning,
not duplicate it.

Decide this when `v1.observability.tasks.instrumentation` designs the composition-root telemetry
layer, not before.

Assumes: the service still runs as a bare host process in local dev when this is decided. If
`go-web-service` has been containerized by then, this concept may already be moot.
