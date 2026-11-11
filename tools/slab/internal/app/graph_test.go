package app_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/graph/graphtest"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-web-service/tools/slab/internal/app"
	"github.com/standards-lab/go-web-service/tools/slab/scenarios"
)

// This file pins the graph's structural properties the composition root
// owns: each Nodes field names its node, and each leaf command's Build
// keeps every order its nodes declare and puts grafana up above service,
// so a failed start names the first thing to fix. It builds the nodes
// without starting them: no constructor dials, since the broker, service,
// and grafana up nodes only bind clients, which the lifecycle's Start
// connects or probes, so it needs nothing running. A failing layer
// assertion prints the System's layer table.

// Each node's name is its Nodes field's name lowercased, ignoring the
// name's spaces, so a field names the node it holds without a comment
// repeating the name, and every field holds a handle once New returns.
// "grafana up" passes as GrafanaUp and "nats stream" as NATSStream.
func TestGraph_NodesNamedByTheirFields(t *testing.T) {
	a := app.New(cli.Streams{})
	graphtest.CheckNames(t, a.Nodes())
}

// layerError returns an error of the formatted message followed by sys's
// layer table.
func layerError(sys *graph.System, format string, args ...any) error {
	return fmt.Errorf(format+"\nlayers:\n%s", append(args, sys)...)
}

// leafLayers builds refs, a leaf's node set, with n's lifecycle config node
// from g, as the dispatcher builds them for a dispatch to the leaf, and
// reports, as an error, a failed Build or, carrying the layer table, what
// is wrong with the System: an After edge the Build dropped, since a
// dropped After means the leaf's path forgot to declare a node something
// it builds is ordered against, so that order is silently lost for the
// leaf's run; or grafana up at or below service's layer when both are
// built, since the lifecycle stops at the first layer with a failure and,
// with both down, would report Grafana's and never probe the service.
func leafLayers(g *graph.Graph, n app.Nodes, refs []graph.Ref) error {
	sys, err := g.Build(append(slices.Clone(refs), n.LifecycleConfig)...)
	if err != nil {
		return fmt.Errorf("Build: %w", err)
	}
	var drops []string
	for _, d := range sys.Dropped() {
		drops = append(drops, fmt.Sprintf("%s's After of %s", d.Node.Name(), d.After.Name()))
	}
	if len(drops) > 0 {
		return layerError(sys, "dropped %s; want every After to order", strings.Join(drops, ", "))
	}
	if !sys.Has(n.Service) || !sys.Has(n.GrafanaUp) {
		return nil
	}
	if gl, sl := sys.LayerOf(n.GrafanaUp), sys.LayerOf(n.Service); gl <= sl {
		return layerError(sys, "%s in layer %d, %s in layer %d; want %s above %s",
			n.GrafanaUp.Name(), gl, n.Service.Name(), sl, n.GrafanaUp.Name(), n.Service.Name())
	}
	return nil
}

// Every leaf of the App's tree builds, from the nodes its path declares and
// the lifecycle config node, a System that drops no order and puts grafana
// up above service wherever it holds both. The leaves are the root's own,
// not a list copied here, so a command or scenario mounted later is
// checked too.
func TestGraph_LeafLayers(t *testing.T) {
	a := app.New(cli.Streams{})
	for path, refs := range a.Root().Leaves() {
		t.Run(path, func(t *testing.T) {
			if err := leafLayers(a.Graph(), a.Nodes(), refs); err != nil {
				t.Error(err)
			}
		})
	}
}

// wantGuardFails fails t unless err, the guard's verdict on a fixture whose
// System is sys, is an error that mentions mention and carries sys's layer
// table.
func wantGuardFails(t *testing.T, err error, sys *graph.System, mention string) {
	t.Helper()
	switch {
	case err == nil:
		t.Fatalf("the guard passed a fixture that breaks it\nlayers:\n%s", sys)
	case !strings.Contains(err.Error(), mention):
		t.Errorf("guard error = %q, want it to mention %q", err, mention)
	case !strings.Contains(err.Error(), "\nlayers:\n"+sys.String()):
		t.Errorf("guard error = %q, want it to carry the layer table:\n%s", err, sys)
	}
}

// The layer guard fails a leaf it should, naming what is wrong and
// carrying the layer table, so the test above can fail. A fixture leaf on
// the App's own tree declares a node ordered after one its path does not
// declare, so the Build drops that order; and a graph whose grafana up
// lacks its After of service builds the two in one layer.
func TestGraph_LeafLayersGuardFails(t *testing.T) {
	t.Run("a dropped order", func(t *testing.T) {
		a := app.New(cli.Streams{})
		undeclared := a.Graph().Value("undeclared", struct{}{})
		ordered := a.Graph().Value("ordered", struct{}{}, graph.After(undeclared))
		a.Root().Add((&cli.Command{
			Name:    "fixture",
			Summary: "Declare a node ordered after an undeclared one",
			Run:     func(context.Context, *cli.Invocation) error { return nil },
		}).Use(ordered))

		var found bool
		for path, refs := range a.Root().Leaves() {
			if path != "slab fixture" {
				continue
			}
			found = true
			err := leafLayers(a.Graph(), a.Nodes(), refs)
			// The same set again, for the table the error should carry.
			sys, buildErr := a.Graph().Build(append(slices.Clone(refs), a.Nodes().LifecycleConfig)...)
			if buildErr != nil {
				t.Fatalf("Build: %v", buildErr)
			}
			wantGuardFails(t, err, sys, "dropped ordered's After of undeclared")
		}
		if !found {
			t.Fatal(`Leaves yielded no "slab fixture" leaf`)
		}
	})

	t.Run("grafana up beside service", func(t *testing.T) {
		g := graph.New()
		n := app.Nodes{
			LifecycleConfig: g.Value("lifecycle config", lifecycle.Config{}),
			Service:         g.Value("service", scenarios.Service{}),
			GrafanaUp:       g.Value("grafana up", scenarios.Grafana{}),
		}
		refs := []graph.Ref{n.Service, n.GrafanaUp}
		err := leafLayers(g, n, refs)
		sys, buildErr := g.Build(append(slices.Clone(refs), n.LifecycleConfig)...)
		if buildErr != nil {
			t.Fatalf("Build: %v", buildErr)
		}
		wantGuardFails(t, err, sys, "want grafana up above service")
	})
}
