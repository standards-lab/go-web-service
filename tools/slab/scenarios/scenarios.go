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

// serviceName is the service.name resource attribute the service exports,
// which Tempo and Loki index the trace under. The same string is stated in
// two other places: internal/app/telemetry.go's serviceName constant, which
// is unexported and so not importable, and
// compose/otel-collector/otel-collector.yaml.
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
	return []scenario.Scenario{Sqlate(n), Domain(n), Problems(n), Storage(n)}
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

// showApart is show set off by a blank line from what precedes it, for the
// scenarios whose blocks follow one another, where a request, its
// response, and what is read off them would otherwise run together. An
// empty caption shows the block alone, as a request or response, whose
// status line heads it, is shown. The blank line is an empty Note, which
// the reporter prints as one blank line, never two and never first.
func showApart(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("")
	if caption == "" {
		return r.Show(block)
	}
	return show(r, caption, block)
}

// call narrates one HTTP call through c: the request as sent, the call,
// and the response as it came, each set apart. A transport failure is
// returned with nothing shown for the response; checking the status is
// the caller's.
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

// showTrace shows the pointer to the trace of the request res answered,
// under grafana, read from res's X-Request-Id header, captioned
// Observability.
func showTrace(r *scenario.Reporter, st style.Style, grafana Grafana, res *httpx.Response) error {
	traceID, err := res.TraceID()
	if err != nil {
		return err
	}
	return showApart(r, "Observability", output.Trace(st, grafana.URL, serviceName, traceID))
}
