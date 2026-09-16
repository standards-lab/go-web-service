// Package scenario defines what a slab scenario is, the reporter it narrates
// through, and the registry the command tree is built from.
package scenario

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"
)

// Scenario is one narrated capability: what it shows, what must already be
// running, and the ordered steps that show it.
type Scenario struct {
	Name    string               // the word after "slab demo"
	Summary string               // the line slab list prints
	Needs   []Need               // preconditions, checked before the first step
	Flags   func(*pflag.FlagSet) // the scenario's own flags; nil for none
	Steps   []Step
}

// Need is one precondition a scenario states and checks for itself: what has
// to be reachable, the mise task that makes it so, and the probe that decides.
type Need struct {
	What  string
	Task  string
	Check func(context.Context) error
}

// Step is one beat of the narration: the sentence saying what is about to
// happen, and the action that happens. An action reports what it observed
// through the reporter it is given.
type Step struct {
	Intent string
	Action func(context.Context, *Reporter) error
}

// Env is what the root command's flags resolved to. Scenarios and their
// checks read it from the context, since a Need's probe receives nothing
// else.
type Env struct {
	Base    string // the service's base URL
	Grafana string // Grafana's base URL
	Tempo   string // Tempo's HTTP API base URL
	Repo    string // the repository root override; empty when not given
}

type envKey struct{}

// WithEnv returns a context carrying env.
func WithEnv(ctx context.Context, env Env) context.Context {
	return context.WithValue(ctx, envKey{}, env)
}

// EnvFrom returns the Env carried by ctx, or the zero Env when none is.
func EnvFrom(ctx context.Context) Env {
	env, _ := ctx.Value(envKey{}).(Env)
	return env
}

// Run checks every need of s, then runs its steps in order, narrating each
// intent through r before its action. It stops at the first need that fails
// or the first action that returns an error.
func Run(ctx context.Context, s Scenario, r *Reporter) error {
	for _, n := range s.Needs {
		if n.Check == nil {
			continue
		}
		if err := n.Check(ctx); err != nil {
			r.Note("%s is not reachable: %v", n.What, err)
			if n.Task != "" {
				r.Note("start it with: mise run %s", n.Task)
			}
			return fmt.Errorf("%s: need %q: %w", s.Name, n.What, err)
		}
	}
	for i, step := range s.Steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.Intent(i+1, len(s.Steps), step.Intent)
		if step.Action == nil {
			continue
		}
		if err := step.Action(ctx, r); err != nil {
			return fmt.Errorf("%s: step %d: %w", s.Name, i+1, err)
		}
	}
	return nil
}
