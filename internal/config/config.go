package config

import (
	"fmt"
	"os"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
)

// envPrefix namespaces the service's environment variables ("app" →
// APP_LOG_LEVEL); a seeded service renames its whole namespace here.
const envPrefix = "app"

const defaultShutdownTimeout = 10 * time.Second

// Config is the service's root configuration: the library capability blocks
// plus the service-owned shutdown timeout.
type Config struct {
	Log             logging.Config     `json:"log"`
	Server          web.Config         `json:"server"`
	Database        database.Config    `json:"database"`
	ShutdownTimeout libconfig.Duration `json:"shutdown_timeout"`
}

// Merge overlays src's set fields onto the receiver, delegating each block
// to its own Merge.
func (c *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	if src.ShutdownTimeout != 0 {
		c.ShutdownTimeout = src.ShutdownTimeout
	}
	c.Log.Merge(&src.Log)
	c.Server.Merge(&src.Server)
	c.Database.Merge(&src.Database)
}

// Finalize applies the root default, reads the root's own environment
// override when a prefix is given, validates, and finalizes each block under
// the same prefix. It satisfies the config package's Load contract; an empty
// prefix disables every environment override — the hermetic form tests use.
func (c *Config) Finalize(envPrefix string) error {
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = libconfig.Duration(defaultShutdownTimeout)
	}

	if envPrefix != "" {
		name := libconfig.EnvName(envPrefix, "shutdown_timeout")
		if v := os.Getenv(name); v != "" {
			if err := c.ShutdownTimeout.Set(v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be positive, got %s", c.ShutdownTimeout)
	}

	if err := c.Log.Finalize(envPrefix); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	if err := c.Server.Finalize(envPrefix); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if err := c.Database.Finalize(envPrefix); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	return nil
}

// Load reads the layered configuration files and finalizes the result under
// the service's env prefix.
func Load() (*Config, error) {
	return libconfig.Load[Config](libconfig.Options{
		EnvPrefix: envPrefix,
	})
}
