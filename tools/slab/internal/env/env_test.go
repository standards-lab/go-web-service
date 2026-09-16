package env_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/env"
)

func TestFromContext_RoundTripsWhatWithContextCarried(t *testing.T) {
	e := env.Env{Base: "http://base", Grafana: "http://grafana", Tempo: "http://tempo", Repo: "/repo"}
	ctx := env.WithContext(context.Background(), e)
	if got := env.FromContext(ctx); got != e {
		t.Errorf("FromContext = %+v, want %+v", got, e)
	}
}

func TestFromContext_IsTheZeroValueWhenNoneWasCarried(t *testing.T) {
	if got := env.FromContext(context.Background()); got != (env.Env{}) {
		t.Errorf("FromContext = %+v, want the zero Env", got)
	}
}
