package config_test

import (
	"strings"
	"testing"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

func TestReadsConfig_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	limits := cfg.Reads.Limits()
	if limits.DefaultSize != 20 {
		t.Errorf("DefaultSize = %d, want 20", limits.DefaultSize)
	}
	if limits.MaxSize != 100 {
		t.Errorf("MaxSize = %d, want 100", limits.MaxSize)
	}
}

func TestReadsConfig_MergeOverlaysSetFields(t *testing.T) {
	ten, forty := 10, 40
	base := &config.ReadsConfig{DefaultSize: &ten}
	overlay := &config.ReadsConfig{MaxSize: &forty}

	base.Merge(overlay)

	// The overlay's set field lands; the field it leaves unset keeps the
	// base value.
	if base.DefaultSize == nil || *base.DefaultSize != 10 {
		t.Errorf("DefaultSize = %v, want 10", base.DefaultSize)
	}
	if base.MaxSize == nil || *base.MaxSize != 40 {
		t.Errorf("MaxSize = %v, want 40", base.MaxSize)
	}
}

func TestReadsConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv(libconfig.EnvName("app", "reads_default_size"), "5")
	t.Setenv(libconfig.EnvName("app", "reads_max_size"), "50")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	limits := cfg.Reads.Limits()
	if limits.DefaultSize != 5 {
		t.Errorf("DefaultSize = %d, want 5", limits.DefaultSize)
	}
	if limits.MaxSize != 50 {
		t.Errorf("MaxSize = %d, want 50", limits.MaxSize)
	}
}

func TestReadsConfig_FinalizeRejectsInvalidSizes(t *testing.T) {
	zero := 0
	cfg := configtest.Minimal()
	cfg.Reads.DefaultSize = &zero
	if err := cfg.Finalize(""); err == nil || !strings.Contains(err.Error(), "default_size") {
		t.Errorf("Finalize with zero default = %v, want default_size error", err)
	}

	ten, five := 10, 5
	cfg = configtest.Minimal()
	cfg.Reads.DefaultSize = &ten
	cfg.Reads.MaxSize = &five
	if err := cfg.Finalize(""); err == nil || !strings.Contains(err.Error(), "max_size") {
		t.Errorf("Finalize with max below default = %v, want max_size error", err)
	}
}
