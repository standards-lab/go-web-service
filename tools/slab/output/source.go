package output

import (
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
)

// Source is how a command on go-cli-sdk reaches the Output it renders
// through. Node is the graph node the color setting is read from, which
// the command declares with Use; New constructs the run's Output over the
// invocation's streams, reading that node, so it is called from the
// command's Run, after the dispatcher's Build. The composition root fills
// it in; a command family receives it and never names a stream, a style,
// or the configuration itself.
type Source struct {
	Node graph.Ref
	New  func(inv *cli.Invocation) *Output
}
