package config_test

import (
	"strings"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

func TestSweepConfig_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	s := cfg.Sweep
	if s.Interval.Duration() != 30*time.Second || *s.Batch != 100 || s.StaleAge.Duration() != time.Hour {
		t.Errorf("sweep = {%s %d %s}, want {30s 100 1h}", s.Interval, *s.Batch, s.StaleAge)
	}
	if week := 7 * 24 * time.Hour; s.OutboxAge.Duration() != week || s.InboxAge.Duration() != week {
		t.Errorf("sweep ages = {outbox %s, inbox %s}, want 168h each", s.OutboxAge, s.InboxAge)
	}
}

func TestSweepConfig_MergeOverlaysSetFields(t *testing.T) {
	ten := 10
	base := &config.SweepConfig{Interval: libconfig.Duration(time.Minute), Batch: &ten}
	base.InboxAge = libconfig.Duration(time.Hour)
	overlay := &config.SweepConfig{StaleAge: libconfig.Duration(2 * time.Hour), OutboxAge: libconfig.Duration(3 * time.Hour)}

	base.Merge(overlay)

	// The overlay's set fields land; the fields it leaves unset keep the
	// base values.
	if base.Interval.Duration() != time.Minute || *base.Batch != 10 || base.StaleAge.Duration() != 2*time.Hour {
		t.Errorf("merged = {%s %d %s}, want {1m 10 2h}", base.Interval, *base.Batch, base.StaleAge)
	}
	if base.OutboxAge.Duration() != 3*time.Hour || base.InboxAge.Duration() != time.Hour {
		t.Errorf("merged ages = {outbox %s, inbox %s}, want {3h 1h}", base.OutboxAge, base.InboxAge)
	}
}

func TestSweepConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv(libconfig.EnvName("app", "sweep_interval"), "5s")
	t.Setenv(libconfig.EnvName("app", "sweep_batch"), "7")
	t.Setenv(libconfig.EnvName("app", "sweep_stale_age"), "90m")
	t.Setenv(libconfig.EnvName("app", "sweep_outbox_age"), "48h")
	t.Setenv(libconfig.EnvName("app", "sweep_inbox_age"), "72h")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	s := cfg.Sweep
	if s.Interval.Duration() != 5*time.Second || *s.Batch != 7 || s.StaleAge.Duration() != 90*time.Minute {
		t.Errorf("sweep = {%s %d %s}, want {5s 7 1h30m}", s.Interval, *s.Batch, s.StaleAge)
	}
	if s.OutboxAge.Duration() != 48*time.Hour || s.InboxAge.Duration() != 72*time.Hour {
		t.Errorf("sweep ages = {outbox %s, inbox %s}, want {48h 72h}", s.OutboxAge, s.InboxAge)
	}
}

func TestSweepConfig_FinalizeRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(*config.Config){
		"interval":   func(c *config.Config) { c.Sweep.Interval = libconfig.Duration(-time.Second) },
		"batch":      func(c *config.Config) { c.Sweep.Batch = new(0) },
		"stale_age":  func(c *config.Config) { c.Sweep.StaleAge = libconfig.Duration(-time.Minute) },
		"outbox_age": func(c *config.Config) { c.Sweep.OutboxAge = libconfig.Duration(-time.Hour) },
		"inbox_age":  func(c *config.Config) { c.Sweep.InboxAge = libconfig.Duration(-time.Hour) },
	}
	for field, set := range cases {
		cfg := configtest.Minimal()
		set(cfg)
		if err := cfg.Finalize(""); err == nil || !strings.Contains(err.Error(), "sweep: "+field) {
			t.Errorf("Finalize with an invalid %s = %v, want a sweep: %s error", field, err, field)
		}
	}

	t.Setenv(libconfig.EnvName("app", "sweep_interval"), "soon")
	if err := configtest.Minimal().Finalize("app"); err == nil || !strings.Contains(err.Error(), "APP_SWEEP_INTERVAL") {
		t.Errorf("Finalize with an unparsable interval = %v, want the variable named", err)
	}

	// An age its variable sets non-positive is refused, naming the key.
	// The variable applies after the defaults, so "0s" lands as zero
	// rather than reading as unset.
	t.Setenv(libconfig.EnvName("app", "sweep_interval"), "")
	for _, key := range []string{"outbox_age", "inbox_age"} {
		for _, v := range []string{"-1h", "0s"} {
			t.Setenv(libconfig.EnvName("app", "sweep_"+key), v)
			if err := configtest.Minimal().Finalize("app"); err == nil || !strings.Contains(err.Error(), "sweep: "+key) {
				t.Errorf("Finalize with %s %s = %v, want a sweep: %s error", key, v, err, key)
			}
			t.Setenv(libconfig.EnvName("app", "sweep_"+key), "")
		}
	}
}
