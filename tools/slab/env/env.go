package env

import "context"

// Env is what the root command's flags resolved to.
type Env struct {
	Base    string // the service's base URL
	Grafana string // Grafana's base URL
	Tempo   string // Tempo's HTTP API base URL
	Repo    string // the repository root override; empty when not given
}

type contextKey struct{}

// WithContext returns a context carrying e.
func WithContext(ctx context.Context, e Env) context.Context {
	return context.WithValue(ctx, contextKey{}, e)
}

// FromContext returns the Env carried by ctx, or the zero Env when none is.
func FromContext(ctx context.Context) Env {
	e, _ := ctx.Value(contextKey{}).(Env)
	return e
}
