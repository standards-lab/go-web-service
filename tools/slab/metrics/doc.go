// Package metrics reads the service's metrics back from Mimir, the
// observability profile's metrics store, as a scenario narrates them. It
// queries Mimir's Prometheus API through Grafana's datasource proxy, at
// the Mimir datasource compose/grafana provisions (uid mimir), so a
// scenario that reads metrics needs only the Grafana it already checks is
// up, and no second base URL.
//
// The service exports its instruments over OTLP to the collector, whose
// prometheus_remote_write exporter writes them to Mimir under Prometheus
// names: dots become underscores, a counter gains _total, a gauge whose
// unit is 1 gains _ratio, and the service.name resource becomes the job
// label. So the counter sweep.passes is sweep_passes_total, its attributes
// the labels sweep_reclaimer and sweep_outcome, and the gauge
// sweep.backlogged is sweep_backlogged_ratio.
//
// The package exports:
//
//   - [DatasourceUID], the Mimir datasource's uid in Grafana
//   - [Client] and [NewClient], an instant query through the proxy
//   - [Sample], one series' value at its sample time
//   - [Client.Await], which polls a query until its samples satisfy a
//     condition, within a limit, and [ErrNotYet], the error its limit
//     passing wraps
//
// client.go holds all of it. output.Metrics renders the pointer a reader
// follows to the same query in Grafana's Explore.
package metrics
