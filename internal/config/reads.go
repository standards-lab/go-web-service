package config

import (
	"fmt"
	"strconv"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk"
)

// The service's paging policy defaults, applied by Finalize when the
// configuration leaves a field unset.
const (
	defaultReadsDefaultSize = 20
	defaultReadsMaxSize     = 100
)

// ReadsConfig is the service-owned read policy: the page size a request gets
// when it asks for none, and the largest size it may ask for. The SDK holds
// no policy numbers; this block is their single source, handed to each
// handler constructor as [web.Limits] at the composition root. Pointer
// fields distinguish unset from zero.
type ReadsConfig struct {
	DefaultSize *int `json:"default_size"`
	MaxSize     *int `json:"max_size"`
}

// Merge overlays src's set fields onto the receiver.
func (c *ReadsConfig) Merge(src *ReadsConfig) {
	if src == nil {
		return
	}
	if src.DefaultSize != nil {
		c.DefaultSize = src.DefaultSize
	}
	if src.MaxSize != nil {
		c.MaxSize = src.MaxSize
	}
}

// Finalize applies the defaults, reads the block's environment overrides
// (APP_READS_DEFAULT_SIZE, APP_READS_MAX_SIZE), and validates: DefaultSize at least 1 and MaxSize
// at least DefaultSize — the same invariant [web.ParseQuery] panics on,
// caught here as configuration rather than at the first request.
func (c *ReadsConfig) Finalize(envPrefix string) error {
	if c.DefaultSize == nil {
		c.DefaultSize = new(defaultReadsDefaultSize)
	}
	if c.MaxSize == nil {
		c.MaxSize = new(defaultReadsMaxSize)
	}

	if err := libconfig.SetFromEnv(&c.DefaultSize, libconfig.EnvName(envPrefix, "reads_default_size"), strconv.Atoi); err != nil {
		return err
	}
	if err := libconfig.SetFromEnv(&c.MaxSize, libconfig.EnvName(envPrefix, "reads_max_size"), strconv.Atoi); err != nil {
		return err
	}

	if *c.DefaultSize < 1 {
		return fmt.Errorf("default_size must be at least 1, got %d", *c.DefaultSize)
	}
	if *c.MaxSize < *c.DefaultSize {
		return fmt.Errorf(
			"max_size must be at least default_size (%d), got %d",
			*c.DefaultSize, *c.MaxSize,
		)
	}
	return nil
}

// Limits hands the finalized policy to a handler constructor. Calling it
// before Finalize is a wiring mistake and panics on the nil fields.
func (c ReadsConfig) Limits() web.Limits {
	return web.Limits{
		DefaultSize: *c.DefaultSize,
		MaxSize:     *c.MaxSize,
	}
}
