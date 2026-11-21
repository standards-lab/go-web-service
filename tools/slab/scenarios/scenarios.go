package scenarios

import (
	"context"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-cli-sdk/scenario"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

const serviceName = "go-web-service"

// Nodes are the graph nodes the scenarios are built over, handed in by the
// composition root: each scenario declares the ones it reads, need nodes
// first and then the settings, and its steps read their values from the
// invocation. The package defines no nodes of its own; it defines the
// values of the two need nodes, [Service] and [Grafana], whose Start
// fails the run before its first step when what they need is down.
type Nodes struct {
	Service   *graph.Node[Service] // the running service, checked ready
	GrafanaUp *graph.Node[Grafana] // the running Grafana, checked up
	Repo      *graph.Node[string]  // the repository root override; empty when not given
	NoColor   *graph.Node[bool]    // whether to print without ANSI color even on a terminal
}

// All returns the scenarios built over n, in the order the scenario
// parent and its listing present them.
func All(n Nodes) []scenario.Scenario {
	return []scenario.Scenario{Sqlate(n), Domain(n), Problems(n), Storage(n), Events(n), Sweeper(n)}
}

func styleOf(n Nodes, inv *cli.Invocation) style.Style {
	return style.New(style.ColorEnabled(inv.Stdout, inv.Get(n.NoColor)))
}

func show(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("")
	r.Note("%s", caption)
	if err := r.Show(block); err != nil {
		return err
	}
	r.Note("")
	return nil
}

func showApart(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("")
	if caption != "" {
		r.Note("%s", caption)
	}
	return r.Show(block)
}

func call(ctx context.Context, r *scenario.Reporter, st style.Style, c *httpx.Client, method, path string, headers []httpx.Header, body any) (*httpx.Response, error) {
	if err := showApart(r, "", output.Request(st, method, path, headers, body)); err != nil {
		return nil, err
	}
	res, err := c.Do(ctx, method, path, body, headers...)
	if err != nil {
		return nil, err
	}
	return res, showApart(r, "", output.Response(st, res))
}

func showTrace(r *scenario.Reporter, st style.Style, grafana Grafana, res *httpx.Response) error {
	traceID, err := res.TraceID()
	if err != nil {
		return err
	}
	return showApart(r, "Observability", output.Trace(st, grafana.URL, serviceName, traceID))
}
