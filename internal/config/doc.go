// Package config declares the service's layered configuration: the [Config]
// root composes the library capability blocks (log, server) with the
// service-owned shutdown timeout, and [Load] reads the layered files and
// finalizes the result under the service's env prefix. The unexported
// envPrefix const is the single place a seeded service renames its
// environment namespace.
package config
