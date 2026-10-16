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
	return []scenario.Scenario{Sqlate(n), Domain(n), Problems(n), Storage(n), Events(n), Sweeper(n)}
}

// styleOf returns the style a step colors its blocks with: color on when
// the invocation's stdout is a terminal and neither NO_COLOR nor the no
// color node says otherwise. A scenario that calls it declares the no
// color node.
func styleOf(n Nodes, inv *cli.Invocation) style.Style {
	return style.New(style.ColorEnabled(inv.Stdout, inv.Get(n.NoColor)))
}

// show prints a captioned SQL, JSON, or table block through r, set off
// from the narration around it the way the old runner set it off: a blank
// line, the caption as a plain note, the block, and a blank line. Only the
// block is colored. Each blank line is an empty Note, which the reporter
// prints as one blank line, never two and never first, so blocks that
// follow one another stand one blank line apart.
func show(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("")
	r.Note("%s", caption)
	if err := r.Show(block); err != nil {
		return err
	}
	r.Note("")
	return nil
}

// showApart prints a block set off by a blank line from what precedes it
// but not from what follows, for an HTTP request, its response, and the
// trace pointer read off them, where the narration after reads on from
// the block. An empty caption shows the block alone, as a request or
// response, whose status line heads it, is shown. The blank line is an
// empty Note, as in show.
func showApart(r *scenario.Reporter, caption string, block output.Render) error {
	r.Note("")
	if caption != "" {
		r.Note("%s", caption)
	}
	return r.Show(block)
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
