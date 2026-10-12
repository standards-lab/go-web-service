package scenario

import (
	"context"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/env"
)

// Source is how a scenario command on go-cli-sdk reaches what its run
// needs. Node is the graph node the environment and the color setting are
// read from, which the command's parent declares with Use; Env resolves the
// run's environment and Reporter constructs the reporter over the
// invocation's stdout, each reading that node, so both are called from the
// command's Run, after the dispatcher's Build. The composition root fills
// it in; a scenario never names the configuration, a stream, or a style
// itself.
type Source struct {
	Node     graph.Ref
	Env      func(inv *cli.Invocation) env.Env
	Reporter func(inv *cli.Invocation) *Reporter
}

// Command builds the go-cli-sdk command that runs s, named for it with its
// summary: it takes no arguments, defines s.Flags on its own flag set, and
// runs s through [Run] with the environment src resolves bound into the
// context, narrating through the reporter src constructs. It declares no
// node itself; the parent it is added to declares src.Node. A failed run's
// error is returned for the dispatcher to report.
func Command(s Scenario, src Source) *cli.Command {
	cmd := &cli.Command{
		Name:    s.Name,
		Summary: s.Summary,
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			return Run(env.WithContext(ctx, src.Env(inv)), s, src.Reporter(inv))
		},
	}
	if s.Flags != nil {
		s.Flags(cmd.Flags())
	}
	return cmd
}
