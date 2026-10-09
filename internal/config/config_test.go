package config_test

import (
	"os"
	"path/filepath"
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
	for _, v := range []string{"-5s", "0s"} {
		t.Setenv("APP_SHUTDOWN_TIMEOUT", v)

		cfg := configtest.Minimal()
		err := cfg.Finalize("app")
		if err == nil {
			t.Fatalf("Finalize accepted shutdown_timeout %s", v)
		}
		if !strings.Contains(err.Error(), "shutdown_timeout must be positive") {
			t.Errorf("error for %s = %v, want shutdown_timeout must be positive", v, err)
		}
	}
}

// A value the duration parser refuses fails the load with the variable's
// name, so the operator sees which setting to fix.
func TestConfig_FinalizeRejectsUnparsableShutdownTimeout(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "soon")

	cfg := configtest.Minimal()
	err := cfg.Finalize("app")
	if err == nil {
		t.Fatal("Finalize accepted an unparsable APP_SHUTDOWN_TIMEOUT")
	}
	if !strings.Contains(err.Error(), "APP_SHUTDOWN_TIMEOUT") {
		t.Errorf("error = %v, want it to name APP_SHUTDOWN_TIMEOUT", err)
	}
}

// The empty prefix composes no variable name, so no environment value
// reaches the shutdown timeout: the hermetic form tests use.
func TestConfig_FinalizeEmptyPrefixReadsNoShutdownOverride(t *testing.T) {
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")

	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the 10s default", got)
	}
}

// minimalFile is a config.json carrying the required fields, plus extra,
// raw JSON members appended at the top level.
func minimalFile(extra string) string {
	body := `"database": {"name": "app"}, "storage": {"container": "c"}, "observability": {"endpoint": "127.0.0.1:4317"}`
	if extra != "" {
		body += ", " + extra
	}
	return "{" + body + "}"
}

// loadDir writes files into a fresh directory and runs Load there, as the
// server binary does from its working directory. APP_ENV and
// APP_SHUTDOWN_TIMEOUT are cleared, then env's KEY=VALUE entries applied,
// so only what a test names reaches the load.
func loadDir(t *testing.T, files map[string]string, env ...string) (*config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	t.Setenv("APP_ENV", "")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	return config.Load()
}

// shutdown_timeout is a top-level key of config.json, and a file that
// omits it takes the 10s default.
func TestLoad_ShutdownTimeoutIsATopLevelKey(t *testing.T) {
	cfg, err := loadDir(t, map[string]string{"config.json": minimalFile(`"shutdown_timeout": "3s"`)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 3s from the file", got)
	}

	cfg, err = loadDir(t, map[string]string{"config.json": minimalFile("")})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the 10s default", got)
	}
}

// APP_SHUTDOWN_TIMEOUT overrides the file's value through Load.
func TestLoad_EnvOverridesShutdownTimeout(t *testing.T) {
	cfg, err := loadDir(t, map[string]string{"config.json": minimalFile(`"shutdown_timeout": "3s"`)}, "APP_SHUTDOWN_TIMEOUT=45s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 45*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 45s from APP_SHUTDOWN_TIMEOUT", got)
	}
}

// An overlay's shutdown_timeout replaces the base's; an overlay that
// leaves it unset keeps the base's.
func TestLoad_OverlayShutdownTimeout(t *testing.T) {
	base := minimalFile(`"shutdown_timeout": "3s"`)
	cfg, err := loadDir(t, map[string]string{
		"config.json":         base,
		"config.staging.json": `{"shutdown_timeout": "7s"}`,
	}, "APP_ENV=staging")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 7*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the overlay's 7s", got)
	}

	cfg, err = loadDir(t, map[string]string{
		"config.json":         base,
		"config.staging.json": `{"log": {"level": "debug"}}`,
	}, "APP_ENV=staging")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the base's 3s kept", got)
	}
}

// The root configuration declares only its JSON keys: a file naming a Go
// field or an internal member, such as Config or Env, is refused as an
// unknown key rather than decoded into the root.
func TestLoad_RejectsUnknownRootKeys(t *testing.T) {
	for _, key := range []string{"Config", "Env"} {
		_, err := loadDir(t, map[string]string{"config.json": minimalFile(`"` + key + `": {}`)})
		if err == nil {
			t.Errorf("Load accepted a %q key", key)
			continue
		}
		if !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), key) {
			t.Errorf("error for %q = %v, want it refused as an unknown field", key, err)
		}
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
