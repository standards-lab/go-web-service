package config

import (
	"fmt"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-observability"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/middleware/rate-limit"
)

// envPrefix namespaces the service's environment variables ("app" →
// APP_LOG_LEVEL); a seeded service renames its whole namespace here.
const envPrefix = "app"

// Config is the service's root configuration: the library capability blocks
// (the object store among them) plus the service-owned reads policy, the
// admin switches, and the sweep's schedule. It embeds the lifecycle
// Coordinator's configuration by value and untagged, so its
// shutdown_timeout is a top-level key and ShutdownTimeout a promoted field;
// Config.Config is that lifecycle block, the one the composition root hands
// the Coordinator.
type Config struct {
	lifecycle.Config

	Log           logging.Config       `json:"log"`
	Server        web.Config           `json:"server"`
	Database      database.Config      `json:"database"`
	Storage       storage.Config       `json:"storage"`
	Observability observability.Config `json:"observability"`
	RateLimit     ratelimit.Config     `json:"rate_limit"`
	Reads         ReadsConfig          `json:"reads"`
	Admin         AdminConfig          `json:"admin"`
	Sweep         SweepConfig          `json:"sweep"`
}

// Merge overlays src's set fields onto the receiver, delegating each block
// to its own Merge.
func (c *Config) Merge(src *Config) {
	if src == nil {
		return
	}
	c.Config.Merge(&src.Config)
	c.Log.Merge(&src.Log)
	c.Server.Merge(&src.Server)
	c.Database.Merge(&src.Database)
	c.Storage.Merge(&src.Storage)
	c.Observability.Merge(&src.Observability)
	c.RateLimit.Merge(&src.RateLimit)
	c.Reads.Merge(&src.Reads)
	c.Admin.Merge(&src.Admin)
	c.Sweep.Merge(&src.Sweep)
}

// Finalize finalizes each block under the same prefix, the lifecycle block
// first, and validates. It satisfies the config package's Load contract.
// The lifecycle block's error returns as it is, unlabelled, since it names
// its own key or variable: it applies the 10s default shutdown timeout,
// reads <PREFIX>_SHUTDOWN_TIMEOUT, and rejects a non-positive timeout, on
// which the lifecycle coordinator panics. An empty prefix composes no
// variable name, so it disables every environment override — the hermetic
// form tests use.
func (c *Config) Finalize(envPrefix string) error {
	if err := c.Config.Finalize(envPrefix); err != nil {
		return err
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
	if err := c.Storage.Finalize(envPrefix); err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if err := c.Observability.Finalize(envPrefix); err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	if err := c.RateLimit.Finalize(envPrefix); err != nil {
		return fmt.Errorf("rate_limit: %w", err)
	}
	if err := c.Reads.Finalize(envPrefix); err != nil {
		return fmt.Errorf("reads: %w", err)
	}
	if err := c.Admin.Finalize(envPrefix); err != nil {
		return fmt.Errorf("admin: %w", err)
	}
	if err := c.Sweep.Finalize(envPrefix); err != nil {
		return fmt.Errorf("sweep: %w", err)
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
