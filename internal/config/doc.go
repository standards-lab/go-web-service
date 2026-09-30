// Package config declares the service's layered configuration. The
// [Config] root composes the library capability blocks (log, server,
// database, storage, observability, rate limit), the service-owned blocks
// ([ReadsConfig] for the reads policy, [AdminConfig] for the admin
// switches, and [SweepConfig] for the sweep's schedule), and the shutdown
// timeout. [Load] reads the layered files and finalizes the result under
// the service's env prefix. The unexported envPrefix const is the single
// place a seeded service renames its environment namespace.
package config
