package config

import (
	"os"

	libconfig "github.com/standards-lab/go-core/config"
)

// AdminConfig is the administrative layer's block: what an environment's
// admin services do on their own. Seed names the state whose set applies
// at every startup, once the schema is current, and on a seed or reset
// request that names no state: the way a deployment initializes its data,
// and the way the local overlay seeds. Empty applies none. A name the
// seeder does not declare fails startup. A pointer field distinguishes
// unset from an explicit empty, so an overlay file's "seed": "" clears the
// base's name; APP_ADMIN_SEED cannot, since an empty environment value is
// unset, as it is for every override.
type AdminConfig struct {
	Seed *string `json:"seed"`
}

// SeedState reports the finalized name; empty is none.
func (c *AdminConfig) SeedState() string {
	if c.Seed == nil {
		return ""
	}
	return *c.Seed
}

// Merge overlays src's set fields onto the receiver.
func (c *AdminConfig) Merge(src *AdminConfig) {
	if src == nil {
		return
	}
	if src.Seed != nil {
		v := *src.Seed
		c.Seed = &v
	}
}

// Finalize applies the default, none, and reads the block's environment
// override under envPrefix (APP_ADMIN_SEED).
func (c *AdminConfig) Finalize(envPrefix string) error {
	if c.Seed == nil {
		c.Seed = new(string)
	}
	if v := os.Getenv(libconfig.EnvName(envPrefix, "admin_seed")); v != "" {
		c.Seed = &v
	}
	return nil
}
