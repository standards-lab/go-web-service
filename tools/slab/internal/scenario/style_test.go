package scenario

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

func TestStyle_OffLeavesTextUntouched(t *testing.T) {
	off := style{on: false}
	sql := "SELECT id FROM organization WHERE code = $1"
	if got := off.sql(sql); got != sql {
		t.Errorf("sql with color off = %q", got)
	}
	doc := "{\n  \"a\": 1\n}"
	if got := off.jsonColor(doc); got != doc {
		t.Errorf("jsonColor with color off = %q", got)
	}
	if got := off.bold("x"); got != "x" {
		t.Errorf("bold with color off = %q", got)
	}
}

func TestStyle_SQLBoldsWholeWordKeywordsOnly(t *testing.T) {
	on := style{on: true}
	got := on.sql("SELECT settings FROM t ORDER BY id")
	want := ansiBold + "SELECT" + ansiReset + " settings " +
		ansiBold + "FROM" + ansiReset + " t " +
		ansiBold + "ORDER" + ansiReset + " " + ansiBold + "BY" + ansiReset + " id"
	if got != want {
		t.Errorf("sql = %q\nwant %q", got, want)
	}
	if strip(got) != "SELECT settings FROM t ORDER BY id" {
		t.Errorf("sql changed the text under the escapes: %q", strip(got))
	}
}

func TestStyle_JSONColorColorsKeysAndValuesAndKeepsTheText(t *testing.T) {
	on := style{on: true}
	doc := "{\n" +
		"  \"name\": \"a <b> c\",\n" +
		"  \"count\": 3,\n" +
		"  \"ok\": true,\n" +
		"  \"none\": null,\n" +
		"  \"items\": [\n" +
		"    \"x\",\n" +
		"    {\n" +
		"      \"deep\": 1.5\n" +
		"    }\n" +
		"  ]\n" +
		"}"
	got := on.jsonColor(doc)
	if strip(got) != doc {
		t.Fatalf("jsonColor changed the text under the escapes:\n%s", strip(got))
	}
	for _, key := range []string{`"name"`, `"count"`, `"ok"`, `"none"`, `"items"`, `"deep"`} {
		if !strings.Contains(got, ansiBlue+key+ansiReset) {
			t.Errorf("key %s is not colored as a key", key)
		}
	}
	for _, val := range []string{`"a <b> c"`, `3`, `"x"`, `1.5`} {
		if !strings.Contains(got, ansiGreen+val+ansiReset) {
			t.Errorf("value %s is not colored as a value", val)
		}
	}
	for _, lit := range []string{"true", "null"} {
		if strings.Contains(got, ansiGreen+lit) || strings.Contains(got, ansiBlue+lit) {
			t.Errorf("literal %s is colored", lit)
		}
	}
}

func TestReporter_HTTPPrintsStatusChosenHeadersAndIndentedBody(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	res := &httpx.Response{
		Status: http.StatusCreated,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"Location":     {"/organizations/1"},
			"Date":         {"never"},
		},
		Body: []byte(`{"id":"1","code":"acme"}`),
	}
	r.HTTP("the response", res)
	got := out.String()
	want := "  the response\n" +
		"    HTTP 201 Created\n" +
		"    Content-Type: application/json\n" +
		"    Location: /organizations/1\n" +
		"\n" +
		"    {\n" +
		"      \"id\": \"1\",\n" +
		"      \"code\": \"acme\"\n" +
		"    }\n"
	if got != want {
		t.Errorf("HTTP printed:\n%s\nwant:\n%s", got, want)
	}
}

func TestReporter_HTTPPrintsANonJSONBodyAsIs(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.HTTP("plain", &httpx.Response{Status: 200, Body: []byte("not json")})
	if !strings.Contains(out.String(), "    not json\n") {
		t.Errorf("HTTP printed:\n%s", out.String())
	}
}

func TestReporter_TableAlignsNames(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Table("fields", [][2]string{{"id", "1"}, {"parent_id", "none"}})
	want := "  fields\n    id         1\n    parent_id  none\n"
	if out.String() != want {
		t.Errorf("Table printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ColorOnWrapsEveryChannel(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, true)
	r.Intent(1, 2, "do a thing")
	r.SQL("the statement", "SELECT 1")
	r.Link("the trace", "http://grafana/x", "Explore, Tempo, paste the id")
	r.Tick("tick %d", 1)
	got := out.String()
	if !strings.Contains(got, ansiBold+ansiCyan+"do a thing"+ansiReset) {
		t.Error("Intent is not styled as a heading")
	}
	if !strings.Contains(got, ansiBold+"SELECT"+ansiReset) {
		t.Error("SQL did not bold its keyword")
	}
	if !strings.Contains(got, ansiCyan+"http://grafana/x"+ansiReset) {
		t.Error("Link is not styled")
	}
	if strip(got) != "\n[1/2] do a thing\n\n  the statement\n    SELECT 1\n\n  the trace\n    http://grafana/x\n    or: Explore, Tempo, paste the id\n    · tick 1\n" {
		t.Errorf("text under the escapes:\n%q", strip(got))
	}
}
