package scenario_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/scenario"
)

func TestRun_StopsAtAFailedNeed(t *testing.T) {
	boom := errors.New("connection refused")
	var ran bool
	s := scenario.Scenario{
		Name: "stub:needy",
		Needs: []scenario.Need{{
			What:  "the service",
			Task:  "serve",
			Check: func(context.Context) error { return boom },
		}},
		Steps: []scenario.Step{{
			Intent: "never reached",
			Action: func(context.Context, *scenario.Reporter) error { ran = true; return nil },
		}},
	}
	var out bytes.Buffer
	err := scenario.Run(context.Background(), s, scenario.NewReporter(&out, false))
	if !errors.Is(err, boom) {
		t.Errorf("Run error = %v; want the need's error", err)
	}
	if ran {
		t.Error("Run ran a step after a need failed")
	}
	if !strings.Contains(out.String(), "start it with: mise run serve") {
		t.Errorf("Run did not name the task that satisfies the need:\n%s", out.String())
	}
}
