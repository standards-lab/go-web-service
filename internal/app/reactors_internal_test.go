package app

import (
	"context"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"

	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

// waitFor bounds the wait for a delivery that should come at once, so a
// wake that never delivers fails the test instead of hanging it.
const waitFor = 2 * time.Second

// The sweep's wake is nudged at construction, so the sweep runs as soon as
// its source starts, not an interval later: a branch marked before a
// restart is swept at startup. The interval here is an hour, so only the
// nudge can deliver within the wait.
func TestSweepWake_DeliversAtStart(t *testing.T) {
	cfg := configtest.Config(t)
	cfg.Sweep.Interval = libconfig.Duration(time.Hour)
	wake := newSweepWake(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	delivered := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- wake.Receive(ctx, func(context.Context, time.Time) error {
			select {
			case delivered <- struct{}{}:
			default:
			}
			return nil
		})
	}()

	select {
	case <-delivered:
	case <-time.After(waitFor):
		t.Fatal("the wake delivered nothing at start, want the construction's nudge")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Receive = %v", err)
	}
}
