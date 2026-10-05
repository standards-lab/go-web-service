package config_test

import (
	"strconv"
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
	baseRequests := 300
	base.RateLimit.Requests = &baseRequests

	overlay := &config.Config{}
	overlay.Log.Level = logging.LevelDebug
	overlay.Server.Host = "127.0.0.1"
	overlay.Database.Host = "db.internal"
	overlayRequests := 50
	overlay.RateLimit.Requests = &overlayRequests

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
	if base.RateLimit.Requests == nil || *base.RateLimit.Requests != 50 {
		t.Errorf("RateLimit.Requests = %v, want 50", base.RateLimit.Requests)
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
	if cfg.RateLimit.Requests == nil || *cfg.RateLimit.Requests != 300 {
		t.Errorf("RateLimit.Requests = %v, want 300", cfg.RateLimit.Requests)
	}
	if cfg.RateLimit.Window == nil || cfg.RateLimit.Window.Duration() != time.Minute {
		t.Errorf("RateLimit.Window = %v, want 1m", cfg.RateLimit.Window)
	}
}

// Every environment-variable name derives from the prefix Finalize receives —
// in production, the one envPrefix const Load passes, the single place a
// seeded service renames — so a renamed prefix reads its own variables and
// ignores the default namespace.
func TestConfig_FinalizeReadsEnvUnderPrefix(t *testing.T) {
	t.Setenv("SVC_LOG_LEVEL", "debug")
	t.Setenv("SVC_SERVER_PORT", "9090")
	t.Setenv("SVC_DATABASE_NAME", "renamed")
	t.Setenv("SVC_RATE_LIMIT_REQUESTS", "50")
	t.Setenv("APP_LOG_LEVEL", "error")
	t.Setenv("APP_SERVER_PORT", "7070")
	t.Setenv("APP_DATABASE_NAME", "default")
	t.Setenv("APP_RATE_LIMIT_REQUESTS", "70")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("svc"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug from SVC_LOG_LEVEL", cfg.Log.Level)
	}
	if cfg.Server.Port == nil || *cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %v, want 9090 from SVC_SERVER_PORT", cfg.Server.Port)
	}
	if cfg.Database.Name != "renamed" {
		t.Errorf("Database.Name = %s, want renamed from SVC_DATABASE_NAME", cfg.Database.Name)
	}
	if cfg.RateLimit.Requests == nil || *cfg.RateLimit.Requests != 50 {
		t.Errorf("RateLimit.Requests = %v, want 50 from SVC_RATE_LIMIT_REQUESTS", cfg.RateLimit.Requests)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("APP_LOG_LEVEL", "debug")
	t.Setenv("APP_SERVER_PORT", "9090")
	t.Setenv("APP_RATE_LIMIT_REQUESTS", "50")

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
	if cfg.RateLimit.Requests == nil || *cfg.RateLimit.Requests != 50 {
		t.Errorf("RateLimit.Requests = %v, want 50", cfg.RateLimit.Requests)
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

// The shipped files load together, strictly: the base, the local overlay,
// and the example secrets, so a key the configuration does not declare
// fails here rather than at a deployment's start.
func TestConfig_ShippedFilesLoad(t *testing.T) {
	t.Setenv("SHIPPED_ENV", "local")
	cfg, err := libconfig.Load[config.Config](libconfig.Options{
		Dir:         "../..",
		EnvVar:      "SHIPPED_ENV",
		SecretsName: "secrets.example.json",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Admin.SeedState() != "default" || cfg.Storage.Options["max_retries"] != "1" || cfg.Storage.Options["try_timeout"] == "" {
		t.Errorf("loaded admin seed %q, storage options %v; want the overlay over the base", cfg.Admin.SeedState(), cfg.Storage.Options)
	}
	// A download's body resumes past a storage try's deadline, and the
	// store's read idle timeout bounds each read with its resumptions, so
	// the base file sets the try below the idle bound: a stalled try
	// resumes once before the store is cut off.
	if try, err := time.ParseDuration(cfg.Storage.Options["try_timeout"]); err != nil || try >= cfg.Storage.ReadIdleTimeout.Duration() {
		t.Errorf("try_timeout = %q (%v), want it below read_idle_timeout, %s", cfg.Storage.Options["try_timeout"], err, cfg.Storage.ReadIdleTimeout.Duration())
	}
}

// The base file's storage bounds fit the server's: a store that stalls on
// every try is refused within write_timeout, so the 503 is written before
// the connection's deadline passes, and azureblob reads a whole upload
// ahead of the store (block_size × concurrency), so a store that stalls
// never stops the body's reads and is never charged to the client as a
// 408. The retry budget is try_timeout per try, max_retries + 1 tries, and
// the SDK's backoff, 800ms doubled after each try.
func TestConfig_BaseStorageBoundsFitTheServer(t *testing.T) {
	cfg, err := libconfig.Load[config.Config](libconfig.Options{Dir: "../..", SecretsName: "secrets.example.json"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	option := func(key string, def int64) int64 {
		v, ok := cfg.Storage.Options[key]
		if !ok {
			return def
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("option %s = %q: %v", key, v, err)
		}
		return n
	}
	try, err := time.ParseDuration(cfg.Storage.Options["try_timeout"])
	if err != nil {
		t.Fatalf("try_timeout: %v", err)
	}
	retries := option("max_retries", 3)
	budget := time.Duration(retries+1) * try
	for i, delay := int64(0), 800*time.Millisecond; i < retries; i, delay = i+1, delay*2 {
		budget += delay
	}
	if write := cfg.Server.WriteTimeout.Duration(); budget >= write {
		t.Errorf("a stalled store holds a call for %s (%d tries of %s with backoff); want it under write_timeout, %s", budget, retries+1, try, write)
	}
	if ahead := option("block_size", 4<<20) * option("concurrency", 4); cfg.Storage.MaxObjectSize > ahead {
		t.Errorf("max_object_size %d exceeds what azureblob reads ahead, %d", cfg.Storage.MaxObjectSize, ahead)
	}
}
