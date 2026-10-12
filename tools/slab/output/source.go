package output

import (
	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
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

// Respond finishes a direct command's Run over one request: it returns
// err, the request's error, when there is one, then [Expect]'s error when
// res does not carry status, and otherwise renders the reply through the
// run's Output with [Output.Response].
func (s Source) Respond(inv *cli.Invocation, res *httpx.Response, err error, status int) error {
	if err != nil {
		return err
	}
	if err := Expect(res, status); err != nil {
		return err
	}
	s.New(inv).Response(res.Status, res.Body)
	return nil
}
