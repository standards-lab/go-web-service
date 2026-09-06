package config_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

func TestAdmin_DefaultsToNoSeeding(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.Admin.SeedEnabled() {
		t.Error("seed enabled by default; want off")
	}
}

func TestAdmin_MergeOverlaysTheSwitch(t *testing.T) {
	on := true
	base := &config.Config{}
	base.Merge(&config.Config{Admin: config.AdminConfig{Seed: &on}})
	if !base.Admin.SeedEnabled() {
		t.Error("overlay did not enable seeding")
	}
	on = false
	if !base.Admin.SeedEnabled() {
		t.Error("merge aliased the overlay's pointer")
	}
	base.Merge(&config.Config{})
	if !base.Admin.SeedEnabled() {
		t.Error("an unset overlay cleared the switch")
	}
}

func TestAdmin_EnvOverride(t *testing.T) {
	t.Setenv("APP_ADMIN_SEED", "true")
	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if !cfg.Admin.SeedEnabled() || cfg.Admin.Env.Seed != "APP_ADMIN_SEED" {
		t.Errorf("seed = %t, env = %q", cfg.Admin.SeedEnabled(), cfg.Admin.Env.Seed)
	}
}

func TestAdmin_RejectsAMalformedOverride(t *testing.T) {
	t.Setenv("APP_ADMIN_SEED", "yes please")
	cfg := configtest.Minimal()
	err := cfg.Finalize("app")
	if err == nil || !strings.Contains(err.Error(), "admin: APP_ADMIN_SEED") {
		t.Errorf("err = %v; want the variable named under the block", err)
	}
}
