package scenarios

import (
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Nodes are the graph nodes the scenarios are built over, handed in by the
// composition root: each scenario declares the ones it reads, and its
// steps read their values from the invocation. The package defines no
// nodes of its own.
type Nodes struct {
	Repo    *graph.Node[string] // the repository root override; empty when not given
	NoColor *graph.Node[bool]   // whether to print without ANSI color even on a terminal
}

// All returns the scenarios built over n, in the order the scenario
// parent and its listing present them.
func All(n Nodes) []scenario.Scenario {
	return []scenario.Scenario{Sqlate(n)}
}

// styleOf returns the style a step colors its blocks with: color on when
// the invocation's stdout is a terminal and neither NO_COLOR nor the no
// color node says otherwise. A scenario that calls it declares the no
// color node.
func styleOf(n Nodes, inv *cli.Invocation) style.Style {
	return style.New(style.ColorEnabled(inv.Stdout, inv.Get(n.NoColor)))
}

// show prints a captioned block through r: the caption as a plain note,
// then the block, so only the block is colored.
func show(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("%s", caption)
	return r.Show(block)
}
