// Package domain is the composition root for the application's domain
// services: stateless logic composed over infrastructure, with no lifecycle
// of its own. Domain services are defined in the module's base-layer domain
// packages — one package per domain layer, under domain/ — and constructed
// here: [New] is where each service's constructor meets the infrastructure
// fields it depends on, one [Domain] field per layer. This package registers
// and composes only; it defines no domain logic.
//
// Domain services own no resource and never run, which is why New takes no
// lifecycle coordinator: there's nothing in scope to register with. A
// package that does own a resource and run belongs in
// internal/infrastructure, if it's built once at startup, or
// internal/reactors, if it reacts to an external occurrence for the life of
// the process.
package domain
