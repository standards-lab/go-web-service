package reactors

import (
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-web-service/internal/domain"
	"github.com/standards-lab/go-web-service/internal/infrastructure"
)

// Reactors composes the application's event-driven entry points. The
// template ships it empty; which reactors it runs, and what they watch, is
// the application author's decision.
type Reactors struct{}

// New constructs the reactors and registers each on lc. It takes infra for
// the transport connections a reactor owns and dom for the domain calls it
// dispatches to — the two halves a reactor joins.
func New(
	infra *infrastructure.Infrastructure,
	dom *domain.Domain,
	lc *lifecycle.Coordinator,
) (*Reactors, error) {
	return &Reactors{}, nil
}
