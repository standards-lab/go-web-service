package scenarios

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Service is the service node's value: the running service's base URL and
// the client bound to it, which the composition root's client node builds,
// so a scenario calls the service through the same client every command
// does. It is a lifecycle Starter, so a scenario that declares the node
// runs no step until the service answers ready; a failed Start is the
// run's failure, which the dispatcher reports labelled with the node's
// name.
type Service struct {
	URL    string        // the service's base URL, from the base setting
	Client *httpx.Client // the client bound to URL, from the client node
}

// Start probes the service's readiness endpoint, which answers 200 only
// once the service's database and storage are up. Its error names the
// mise task that fixes what it found: nothing answering is the service
// not running, and a 503 is the service up over a database or storage
// that is not.
func (s Service) Start(ctx context.Context) error {
	err := httpx.Probe(ctx, s.URL, web.ReadyPath)
	var status *httpx.StatusError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ended(ctx)
	case errors.As(err, &status) && status.Status == http.StatusServiceUnavailable:
		return fmt.Errorf("%s answers but is not ready, its database or storage is down (start them with: mise run db:up): %w", s.URL, err)
	case errors.As(err, &status):
		return fmt.Errorf("%s answers but is not ready: %w", s.URL, err)
	default:
		return fmt.Errorf("nothing answers at %s (start the service with: mise run serve): %w", s.URL, err)
	}
}

// Grafana is the grafana up node's value: the running Grafana's base URL,
// which a step's trace pointer names, and the client the composition root
// binds to it, which the sweeper scenario reads Mimir through (Grafana's
// datasource proxy; see the metrics package). It is a lifecycle Starter,
// as [Service] is, so a scenario that points at traces or reads metrics
// runs no step until Grafana answers.
type Grafana struct {
	URL    string        // Grafana's base URL, from the grafana setting
	Client *httpx.Client // a client bound to URL
}

// Start probes Grafana's health endpoint. Its error names the mise task
// that brings the observability profile up, Grafana among it.
func (g Grafana) Start(ctx context.Context) error {
	if err := httpx.ProbeGrafana(ctx, g.URL); err != nil {
		if ctx.Err() != nil {
			return ended(ctx)
		}
		return fmt.Errorf("Grafana is not up at %s (start it with: mise run otel:up): %w", g.URL, err)
	}
	return nil
}

// ended is a Start's error when the run context ended during its probe, by
// a signal or by another need node's failed start in the same layer. It
// returns the context's error, which the lifecycle drops once a failure is
// on record, so the cancelled probe adds no second report naming a task
// that would fix nothing. The probe's own error wraps the context's cause,
// and the lifecycle would report it.
func ended(ctx context.Context) error {
	return ctx.Err()
}
