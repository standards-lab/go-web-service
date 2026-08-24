package domain

import (
	"github.com/standards-lab/go-web-service/internal/infrastructure"
)

// Domain composes the application's domain services. The template ships it
// empty; which services it composes is the application author's decision.
type Domain struct{}

// New wires the domain layer over infra.
func New(infra *infrastructure.Infrastructure) *Domain {
	return &Domain{}
}
