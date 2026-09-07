package config

import (
	"os"

	libconfig "github.com/standards-lab/go-core/config"
)

// AdminConfig is the administrative layer's block: what an environment's
// admin services do on their own. Seed names the state whose set applies
// at every startup, once the schema is current, and on a seed request
// that names no set: the way a deployment initializes its data, and the
// way the local overlay seeds. Empty applies none. A name the seeder does
// not declare fails startup. A pointer field distinguishes unset from an
// explicit empty, so an overlay can clear the base's name.
type AdminConfig struct {
	Seed *string  `json:"seed"`
	Env  AdminEnv `json:"-"`
}

// AdminEnv records the environment-variable names Finalize composed and
// read, so a README or a log can name them.
type AdminEnv struct {
	Seed string
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
// override under envPrefix (APP_ADMIN_SEED); an empty prefix disables the
// override.
func (c *AdminConfig) Finalize(envPrefix string) error {
	if c.Seed == nil {
		c.Seed = new(string)
	}
	if envPrefix == "" {
		return nil
	}
	c.Env.Seed = libconfig.EnvName(envPrefix, "admin_seed")
	if v := os.Getenv(c.Env.Seed); v != "" {
		c.Seed = &v
	}
	return nil
}
