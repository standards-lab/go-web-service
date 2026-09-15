package grafana_test

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/standards-lab/go-web-service/tools/slab/internal/grafana"
)

const (
	base    = "http://localhost:3000"
	traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
)

// The golden URLs and pane JSON below were built against Grafana 13.2.2's
// Explore URL scheme (the panes parameter with schemaVersion 1, in use since
// 10.2) from its documented state shape. They have NOT yet been opened in a
// live Grafana; that check is a manual one that follows this stage. When it
// changes what Grafana wants, change the builder and these values together.
var golden = []struct {
	name  string
	url   string
	want  string // the exact URL
	panes string // the decoded panes parameter
}{
	{
		name:  "trace",
		url:   grafana.Trace(base, traceID),
		want:  "http://localhost:3000/explore?orgId=1&panes=%7B%22slab%22%3A%7B%22datasource%22%3A%22tempo%22%2C%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22datasource%22%3A%7B%22type%22%3A%22tempo%22%2C%22uid%22%3A%22tempo%22%7D%2C%22queryType%22%3A%22traceql%22%2C%22query%22%3A%224bf92f3577b34da6a3ce929d0e0e4736%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%22now-1h%22%2C%22to%22%3A%22now%22%7D%7D%7D&schemaVersion=1",
		panes: `{"slab":{"datasource":"tempo","queries":[{"refId":"A","datasource":{"type":"tempo","uid":"tempo"},"queryType":"traceql","query":"4bf92f3577b34da6a3ce929d0e0e4736"}],"range":{"from":"now-1h","to":"now"}}}`,
	},
	{
		name:  "trace logs",
		url:   grafana.TraceLogs(base, traceID),
		want:  "http://localhost:3000/explore?orgId=1&panes=%7B%22slab%22%3A%7B%22datasource%22%3A%22loki%22%2C%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22loki%22%7D%2C%22editorMode%22%3A%22code%22%2C%22queryType%22%3A%22range%22%2C%22expr%22%3A%22%7Bservice_name%3D%5C%22go-web-service%5C%22%7D%20%7C%20trace_id%3D%5C%224bf92f3577b34da6a3ce929d0e0e4736%5C%22%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%22now-1h%22%2C%22to%22%3A%22now%22%7D%7D%7D&schemaVersion=1",
		panes: `{"slab":{"datasource":"loki","queries":[{"refId":"A","datasource":{"type":"loki","uid":"loki"},"editorMode":"code","queryType":"range","expr":"{service_name=\"go-web-service\"} | trace_id=\"4bf92f3577b34da6a3ce929d0e0e4736\""}],"range":{"from":"now-1h","to":"now"}}}`,
	},
	{
		name:  "tempo over an absolute range",
		url:   grafana.Tempo(base, `{resource.service.name="go-web-service"}`, absolute()),
		want:  "http://localhost:3000/explore?orgId=1&panes=%7B%22slab%22%3A%7B%22datasource%22%3A%22tempo%22%2C%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22datasource%22%3A%7B%22type%22%3A%22tempo%22%2C%22uid%22%3A%22tempo%22%7D%2C%22queryType%22%3A%22traceql%22%2C%22query%22%3A%22%7Bresource.service.name%3D%5C%22go-web-service%5C%22%7D%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221789473600000%22%2C%22to%22%3A%221789473690000%22%7D%7D%7D&schemaVersion=1",
		panes: `{"slab":{"datasource":"tempo","queries":[{"refId":"A","datasource":{"type":"tempo","uid":"tempo"},"queryType":"traceql","query":"{resource.service.name=\"go-web-service\"}"}],"range":{"from":"1789473600000","to":"1789473690000"}}}`,
	},
	{
		name:  "loki over an absolute range",
		url:   grafana.Loki(base, grafana.LogSelector, absolute()),
		want:  "http://localhost:3000/explore?orgId=1&panes=%7B%22slab%22%3A%7B%22datasource%22%3A%22loki%22%2C%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22loki%22%7D%2C%22editorMode%22%3A%22code%22%2C%22queryType%22%3A%22range%22%2C%22expr%22%3A%22%7Bservice_name%3D%5C%22go-web-service%5C%22%7D%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221789473600000%22%2C%22to%22%3A%221789473690000%22%7D%7D%7D&schemaVersion=1",
		panes: `{"slab":{"datasource":"loki","queries":[{"refId":"A","datasource":{"type":"loki","uid":"loki"},"editorMode":"code","queryType":"range","expr":"{service_name=\"go-web-service\"}"}],"range":{"from":"1789473600000","to":"1789473690000"}}}`,
	},
}

// absolute is a 90-second range starting 2026-09-15T12:00:00Z, which is
// 1789473600000 in epoch milliseconds.
func absolute() grafana.Range {
	from := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	return grafana.Between(from, from.Add(90*time.Second))
}

func TestExploreGolden(t *testing.T) {
	for _, g := range golden {
		t.Run(g.name, func(t *testing.T) {
			if g.url != g.want {
				t.Errorf("url =\n%s\nwant\n%s", g.url, g.want)
			}
			u, err := url.Parse(g.url)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if got := q.Get("orgId"); got != "1" {
				t.Errorf("orgId = %q, want 1", got)
			}
			if got := q.Get("schemaVersion"); got != "1" {
				t.Errorf("schemaVersion = %q, want 1", got)
			}
			if got := q.Get("panes"); got != g.panes {
				t.Errorf("panes =\n%s\nwant\n%s", got, g.panes)
			}
			if !json.Valid([]byte(g.panes)) {
				t.Errorf("golden panes is not valid JSON: %s", g.panes)
			}
		})
	}
}

func TestExploreBase(t *testing.T) {
	with := grafana.Trace(base+"/", traceID)
	without := grafana.Trace(base, traceID)
	if with != without {
		t.Errorf("a trailing slash on the base changed the URL:\n%s\n%s", with, without)
	}
}

func TestBetween(t *testing.T) {
	got := absolute()
	want := grafana.Range{From: "1789473600000", To: "1789473690000"}
	if got != want {
		t.Errorf("Between = %+v, want %+v", got, want)
	}
}

func TestTraceLogsExpr(t *testing.T) {
	got := grafana.TraceLogsExpr(traceID)
	want := `{service_name="go-web-service"} | trace_id="4bf92f3577b34da6a3ce929d0e0e4736"`
	if got != want {
		t.Errorf("TraceLogsExpr = %q, want %q", got, want)
	}
}

// A query's comparison operators must reach Grafana as written, not as the
// unicode escape (backslash-u003e for >) encoding/json writes by default.
func TestExploreNoHTMLEscape(t *testing.T) {
	u, err := url.Parse(grafana.Tempo(base, `{duration > 100ms}`, absolute()))
	if err != nil {
		t.Fatal(err)
	}
	var panes map[string]struct {
		Queries []struct {
			Query string `json:"query"`
		} `json:"queries"`
	}
	if err := json.Unmarshal([]byte(u.Query().Get("panes")), &panes); err != nil {
		t.Fatal(err)
	}
	if got := panes["slab"].Queries[0].Query; got != `{duration > 100ms}` {
		t.Errorf("query = %q, want the > as written", got)
	}
}
