package config_test

import (
	"strings"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/config/configtest"
)

func TestConfig_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{ShutdownTimeout: libconfig.Duration(10 * time.Second)}
	base.Log.Level = logging.LevelInfo
	base.Server.Host = "0.0.0.0"
	base.Database.Name = "app"

	overlay := &config.Config{}
	overlay.Log.Level = logging.LevelDebug
	overlay.Server.Host = "127.0.0.1"
	overlay.Database.Host = "db.internal"

	base.Merge(overlay)

	if base.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", base.Log.Level)
	}
	if base.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %s, want 127.0.0.1", base.Server.Host)
	}
	if base.Database.Host != "db.internal" {
		t.Errorf("Database.Host = %s, want db.internal", base.Database.Host)
	}
	// A field the overlay leaves unset keeps the base value.
	if base.Database.Name != "app" {
		t.Errorf("Database.Name = %s, want app", base.Database.Name)
	}
	if got := base.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
}

func TestConfig_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Pins the documented default shutdown timeout.
	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
	if cfg.Log.Level != logging.LevelInfo {
		t.Errorf("Log.Level = %s, want info", cfg.Log.Level)
	}
	if got := cfg.Server.Addr(); got != "0.0.0.0:8080" {
		t.Errorf("Server.Addr() = %s, want 0.0.0.0:8080", got)
	}
}

// Every environment-variable name derives from the prefix Finalize receives —
// in production, the one envPrefix const Load passes, the single place a
// seeded service renames.
func TestConfig_FinalizeSeedsEnvNamesFromPrefix(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Log.Env.Level; got != "APP_LOG_LEVEL" {
		t.Errorf("Log.Env.Level = %s, want APP_LOG_LEVEL", got)
	}
	if got := cfg.Server.Env.Port; got != "APP_SERVER_PORT" {
		t.Errorf("Server.Env.Port = %s, want APP_SERVER_PORT", got)
	}
	if got := cfg.Database.Env.Name; got != "APP_DATABASE_NAME" {
		t.Errorf("Database.Env.Name = %s, want APP_DATABASE_NAME", got)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("APP_LOG_LEVEL", "debug")
	t.Setenv("APP_SERVER_PORT", "9090")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("app"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.ShutdownTimeout.Duration(); got != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", got)
	}
	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", cfg.Log.Level)
	}
	if cfg.Server.Port == nil || *cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %v, want 9090", cfg.Server.Port)
	}
}

func TestConfig_FinalizeRejectsNonPositiveShutdownTimeout(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "-5s")

	cfg := configtest.Minimal()
	err := cfg.Finalize("app")
	if err == nil {
		t.Fatal("Finalize accepted a negative shutdown_timeout")
	}
	if !strings.Contains(err.Error(), "shutdown_timeout") {
		t.Errorf("error = %v, want it to name shutdown_timeout", err)
	}
}

func TestConfig_FinalizeWrapsChildErrors(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "verbose")

	cfg := configtest.Minimal()
	err := cfg.Finalize("app")
	if err == nil {
		t.Fatal("Finalize accepted an invalid log level")
	}
	if !strings.Contains(err.Error(), "log:") {
		t.Errorf("error = %v, want the log block wrap", err)
	}
}

func TestConfig_FinalizeRequiresDatabaseName(t *testing.T) {
	cfg := &config.Config{}
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted a config with no database name")
	}
	if !strings.Contains(err.Error(), "database:") {
		t.Errorf("error = %v, want the database block wrap", err)
	}
}
