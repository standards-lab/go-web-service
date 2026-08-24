// Package domain is the composition root for the application's domain
// services: stateless logic composed over infrastructure, with no lifecycle
// of its own. [New] is where a service's constructor meets the
// infrastructure fields it depends on — the template ships no service, so
// New returns an empty [Domain]; an application built from the template adds
// fields to Domain and constructs them here.
//
// Domain services own no resource and never run, which is why New takes no
// lifecycle coordinator: there's nothing in scope to register with. A
// package that does own a resource and run belongs in
// internal/infrastructure, if it's built once at startup, or
// internal/reactors, if it reacts to an external occurrence for the life of
// the process.
package domain
