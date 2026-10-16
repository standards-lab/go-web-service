package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
)

// The sweep's defaults, applied by Finalize when the configuration leaves
// a field unset.
const (
	defaultSweepInterval  = 30 * time.Second
	defaultSweepBatch     = 100
	defaultSweepStaleAge  = time.Hour
	defaultSweepOutboxAge = 7 * 24 * time.Hour
	defaultSweepInboxAge  = 7 * 24 * time.Hour
)

// SweepConfig is the sweep's block, the policy of the general sweeper's
// reclaimers: Interval is how often a reclaimer runs when nothing nudges
// it, Batch the records one pass of blobfs's sweep handles, and StaleAge
// the age past which a pending or deleting file row counts as stale and is
// reclaimed. StaleAge must exceed the longest upload the service lets run,
// counted from its first step. OutboxAge is the age past which the outbox
// reclaimer deletes a published outbox row, and InboxAge the age past which
// the inbox reclaimer deletes a handled inbox row; InboxAge must outlast
// every redelivery the broker can make, since a redelivered event whose row
// is gone is handled again. Batch bounds the outbox's and the inbox's passes
// as it does blobfs's. A zero duration and a nil Batch are unset.
type SweepConfig struct {
	Interval  libconfig.Duration `json:"interval"`
	Batch     *int               `json:"batch"`
	StaleAge  libconfig.Duration `json:"stale_age"`
	OutboxAge libconfig.Duration `json:"outbox_age"`
	InboxAge  libconfig.Duration `json:"inbox_age"`
}

// Merge overlays src's set fields onto the receiver.
func (c *SweepConfig) Merge(src *SweepConfig) {
	if src == nil {
		return
	}
	if src.Interval != 0 {
		c.Interval = src.Interval
	}
	if src.Batch != nil {
		c.Batch = src.Batch
	}
	if src.StaleAge != 0 {
		c.StaleAge = src.StaleAge
	}
	if src.OutboxAge != 0 {
		c.OutboxAge = src.OutboxAge
	}
	if src.InboxAge != 0 {
		c.InboxAge = src.InboxAge
	}
}

// Finalize applies the defaults, reads the block's environment overrides
// (APP_SWEEP_INTERVAL, APP_SWEEP_BATCH, APP_SWEEP_STALE_AGE,
// APP_SWEEP_OUTBOX_AGE, APP_SWEEP_INBOX_AGE), and validates: every duration
// positive and the batch at least 1, the bounds blobfs's sweep and
// go-messaging's retention refuse a pass or a reclaimer outside.
func (c *SweepConfig) Finalize(envPrefix string) error {
	if c.Interval == 0 {
		c.Interval = libconfig.Duration(defaultSweepInterval)
	}
	if c.Batch == nil {
		c.Batch = new(defaultSweepBatch)
	}
	if c.StaleAge == 0 {
		c.StaleAge = libconfig.Duration(defaultSweepStaleAge)
	}
	if c.OutboxAge == 0 {
		c.OutboxAge = libconfig.Duration(defaultSweepOutboxAge)
	}
	if c.InboxAge == 0 {
		c.InboxAge = libconfig.Duration(defaultSweepInboxAge)
	}

	if err := overrideDuration(envPrefix, "sweep_interval", &c.Interval); err != nil {
		return err
	}
	if err := libconfig.SetFromEnv(&c.Batch, libconfig.EnvName(envPrefix, "sweep_batch"), strconv.Atoi); err != nil {
		return err
	}
	if err := overrideDuration(envPrefix, "sweep_stale_age", &c.StaleAge); err != nil {
		return err
	}
	if err := overrideDuration(envPrefix, "sweep_outbox_age", &c.OutboxAge); err != nil {
		return err
	}
	if err := overrideDuration(envPrefix, "sweep_inbox_age", &c.InboxAge); err != nil {
		return err
	}

	if c.Interval <= 0 {
		return fmt.Errorf("interval must be positive, got %s", c.Interval)
	}
	if *c.Batch < 1 {
		return fmt.Errorf("batch must be at least 1, got %d", *c.Batch)
	}
	if c.StaleAge <= 0 {
		return fmt.Errorf("stale_age must be positive, got %s", c.StaleAge)
	}
	if c.OutboxAge <= 0 {
		return fmt.Errorf("outbox_age must be positive, got %s", c.OutboxAge)
	}
	if c.InboxAge <= 0 {
		return fmt.Errorf("inbox_age must be positive, got %s", c.InboxAge)
	}
	return nil
}

// overrideDuration applies one duration environment override onto dst,
// named by the prefix and key under the config package's naming scheme.
func overrideDuration(envPrefix, key string, dst *libconfig.Duration) error {
	name := libconfig.EnvName(envPrefix, key)
	if err := dst.Set(os.Getenv(name)); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
