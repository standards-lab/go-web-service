package config_test

import (
	"testing"

	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

func TestAdmin_DefaultsToNoSet(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := cfg.Admin.SeedState(); got != "" {
		t.Errorf("seed state = %q by default; want none", got)
	}
}

func TestAdmin_MergeOverlaysTheName(t *testing.T) {
	name := "default"
	base := &config.Config{}
	base.Merge(&config.Config{Admin: config.AdminConfig{Seed: &name}})
	if base.Admin.SeedState() != "default" {
		t.Error("overlay did not set the name")
	}
	name = "other"
	if base.Admin.SeedState() != "default" {
		t.Error("merge aliased the overlay's pointer")
	}
	base.Merge(&config.Config{})
	if base.Admin.SeedState() != "default" {
		t.Error("an unset overlay cleared the name")
	}
	empty := ""
	base.Merge(&config.Config{Admin: config.AdminConfig{Seed: &empty}})
	if base.Admin.SeedState() != "" {
		t.Error("an explicit empty overlay did not clear the name")
	}
}

func TestAdmin_EnvOverride(t *testing.T) {
	t.Setenv("APP_ADMIN_SEED", "empty")
	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.Admin.SeedState() != "empty" || cfg.Admin.Env.Seed != "APP_ADMIN_SEED" {
		t.Errorf("seed state = %q, env = %q", cfg.Admin.SeedState(), cfg.Admin.Env.Seed)
	}
}
