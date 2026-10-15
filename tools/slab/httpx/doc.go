// Package httpx is the HTTP client slab's commands and scenarios call the
// service through. It holds everything about HTTP that is not specific to the
// service, Grafana's health probe and the Mimir queries a scenario reads the
// service's metrics with among it.
//
// The package exports:
//
//   - [Client] and [NewClient], which send a request to the service
//   - [Response], one exchange as observed, which output.Response renders
//   - [Header] and [IfMatch], a request header and the precondition one
//   - [RawQuery], which joins query pairs as the printed request shows them
//   - [Probe] and [ProbeGrafana], which probe an endpoint at a base URL, and
//     [StatusError], a probe answered with a status other than 200
//   - [Mimir] and [NewMimir], instant queries to Mimir through Grafana's
//     datasource proxy, [Sample], one series of a query's vector, and
//     [Mimir.Await], which polls a query until its samples satisfy a
//     condition, within a limit
//
// The service exports its instruments over OTLP to the collector, whose
// prometheus_remote_write exporter writes them to Mimir under Prometheus
// names: dots become underscores, a counter gains _total, a gauge whose
// unit is 1 gains _ratio, and the service.name resource becomes the job
// label. So the counter sweep.passes is sweep_passes_total, its attributes
// the labels sweep_reclaimer and sweep_outcome, and the gauge
// sweep.backlogged is sweep_backlogged_ratio. output.Metrics renders the
// pointer a reader follows to the same query in Grafana's Explore.
package httpx
