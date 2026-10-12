package httpx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/env"
)

// probe is the client Probe sends with: a short timeout, so a service that
// has bound its port but not yet served answers false on this poll and
// true on a later one, and its own connection, so a probe never holds a
// scenario client's one connection.
var probe = newHTTPClient(time.Second)

// grafanaHealthPath is Grafana's health endpoint, which answers 200 once
// Grafana serves.
const grafanaHealthPath = "/api/health"

// StatusError is a probe's answer when the endpoint answered with a status
// other than 200: the path probed and the status it answered, so a caller
// can tell a server that answers but is not ready from one that does not
// answer at all, which is the transport's error instead.
type StatusError struct {
	Path   string
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: status %d %s", e.Path, e.Status, http.StatusText(e.Status))
}

// Probe returns nil when base answers GET path with 200, and otherwise the
// reason it did not: a [*StatusError] naming the status it answered
// instead, or the transport's error when nothing answered. It sends one
// request with a one-second timeout and discards the body.
func Probe(ctx context.Context, base, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	res, err := probe.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &StatusError{Path: path, Status: res.StatusCode}
	}
	return nil
}

// ProbeGrafana is [Probe] of Grafana at base on its health endpoint.
func ProbeGrafana(ctx context.Context, base string) error {
	return Probe(ctx, base, grafanaHealthPath)
}

// Live is [Probe] of the service named in ctx's env on its liveness
// endpoint. It is the check a demo scenario's Need states for the service,
// so it takes only the Need's context and reads the service's base URL
// from it.
func Live(ctx context.Context) error {
	return Probe(ctx, env.FromContext(ctx).Base, web.HealthPath)
}

// GrafanaLive is [ProbeGrafana] of the Grafana named in ctx's env. It is
// the check a demo scenario's Need states for Grafana.
func GrafanaLive(ctx context.Context) error {
	return ProbeGrafana(ctx, env.FromContext(ctx).Grafana)
}
