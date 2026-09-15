// Package grafana builds the Explore deep links a scenario prints: one URL
// that opens Grafana's Explore view on a datasource with a query already
// run over a time range.
//
// Grafana 13's Explore keeps its state in the panes query parameter, a JSON
// object keyed by pane id, beside schemaVersion. The older left parameter
// still loads, but Grafana migrates it through a generic query model that
// drops a datasource-specific field such as a TraceQL query, so the links
// here use panes only.
package grafana

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The datasource uids the compose observability profile provisions, from
// compose/observability/grafana/provisioning/datasources/datasources.yaml.
const (
	TempoUID = "tempo"
	LokiUID  = "loki"
)

// ServiceName is the service.name resource attribute the service exports,
// which Loki's OTLP ingestion stores as the service_name stream label. slab
// does not depend on the service's module, so the value is repeated here
// from internal/app/telemetry.go's serviceName constant.
const ServiceName = "go-web-service"

// LogSelector is the LogQL stream selector for the service's logs.
const LogSelector = `{service_name="` + ServiceName + `"}`

// paneID keys the one pane every link opens; Grafana accepts any string.
const paneID = "slab"

// Range is the time range a link opens on. Grafana reads each bound as
// either a relative time such as "now-1h" or epoch milliseconds as a
// string.
type Range struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Between returns the absolute range from from to to, in the epoch
// milliseconds Grafana reads.
func Between(from, to time.Time) Range {
	return Range{From: strconv.FormatInt(from.UnixMilli(), 10), To: strconv.FormatInt(to.UnixMilli(), 10)}
}

// lastHour is the range a trace link opens on: wide enough to hold a trace
// a scenario just produced, narrow enough that Tempo's search stays quick.
var lastHour = Range{From: "now-1h", To: "now"}

// Trace returns the Explore URL under base that runs traceID as a TraceQL
// query against Tempo over the last hour.
func Trace(base, traceID string) string {
	return Tempo(base, traceID, lastHour)
}

// TraceLogs returns the Explore URL under base that runs the service's logs
// filtered to traceID against Loki over the last hour.
func TraceLogs(base, traceID string) string {
	return Loki(base, TraceLogsExpr(traceID), lastHour)
}

// TraceLogsExpr is the LogQL query selecting the service's log lines that
// carry traceID. Loki holds the id as structured metadata named trace_id,
// which LogQL filters as a label after the stream selector.
func TraceLogsExpr(traceID string) string {
	return LogSelector + ` | trace_id="` + traceID + `"`
}

// Tempo returns the Explore URL under base that runs query, TraceQL, against
// the Tempo datasource over r.
func Tempo(base, query string, r Range) string {
	return explore(base, pane{
		Datasource: TempoUID,
		Queries: []paneQuery{{
			RefID:      "A",
			Datasource: datasourceRef{Type: "tempo", UID: TempoUID},
			QueryType:  "traceql",
			Query:      query,
		}},
		Range: r,
	})
}

// Loki returns the Explore URL under base that runs expr, LogQL, against the
// Loki datasource over r as a range query.
func Loki(base, expr string, r Range) string {
	return explore(base, pane{
		Datasource: LokiUID,
		Queries: []paneQuery{{
			RefID:      "A",
			Datasource: datasourceRef{Type: "loki", UID: LokiUID},
			EditorMode: "code",
			QueryType:  "range",
			Expr:       expr,
		}},
		Range: r,
	})
}

// pane is one Explore pane's state as the panes parameter carries it. The
// field order is the JSON order, which the golden test pins.
type pane struct {
	Datasource string      `json:"datasource"`
	Queries    []paneQuery `json:"queries"`
	Range      Range       `json:"range"`
}

// paneQuery is one query in a pane. Tempo reads queryType and query; Loki
// reads editorMode, queryType, and expr; the fields the other does not use
// are omitted from its JSON.
type paneQuery struct {
	RefID      string        `json:"refId"`
	Datasource datasourceRef `json:"datasource"`
	EditorMode string        `json:"editorMode,omitempty"`
	QueryType  string        `json:"queryType"`
	Query      string        `json:"query,omitempty"`
	Expr       string        `json:"expr,omitempty"`
}

type datasourceRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

// explore builds the Explore URL opening p as its one pane. The query
// string comes from url.Values.Encode, which sorts its keys, so the same
// inputs always produce the same URL. The pane JSON is encoded without
// HTML escaping so a query's < or > survives as written. Encode writes a
// space as +, which not every query-string parser reads back as a space;
// since Encode writes a literal + as %2B, every + it leaves is a space,
// and rewriting it to %20 removes the ambiguity.
func explore(base string, p pane) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// A pane holds only strings and structs of strings, which cannot fail
	// to encode.
	_ = enc.Encode(map[string]pane{paneID: p})
	values := url.Values{
		"orgId":         {"1"},
		"panes":         {string(bytes.TrimRight(buf.Bytes(), "\n"))},
		"schemaVersion": {"1"},
	}
	query := strings.ReplaceAll(values.Encode(), "+", "%20")
	return strings.TrimRight(base, "/") + "/explore?" + query
}
