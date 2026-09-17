package scenario

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
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

func TestReporter_RequestPrintsLineHeadersAndEncodedBody(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Request(http.MethodPut, "/organizations/1", []httpx.Header{{Name: "If-Match", Value: `"3"`}}, map[string]string{"code": "acme"})
	want := "\n" +
		"  PUT /organizations/1\n" +
		"    Content-Type: application/json\n" +
		"    If-Match: \"3\"\n" +
		"\n" +
		"    {\n" +
		"      \"code\": \"acme\"\n" +
		"    }\n" +
		"\n"
	if out.String() != want {
		t.Errorf("Request printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_RequestWithNoBodyPrintsNoContentType(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Request(http.MethodDelete, "/organizations/1", []httpx.Header{{Name: "If-Match", Value: `"3"`}}, nil)
	want := "\n  DELETE /organizations/1\n    If-Match: \"3\"\n\n"
	if out.String() != want {
		t.Errorf("Request printed:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	r.Request(http.MethodGet, "/organizations", nil, nil)
	if want := "  GET /organizations\n\n"; out.String() != want {
		t.Errorf("Request printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_RequestPrintsARawBodyAsIsAndKeepsACallerContentType(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Request(http.MethodPost, "/x", []httpx.Header{{Name: "content-type", Value: "text/plain"}}, "not json")
	want := "\n  POST /x\n    content-type: text/plain\n\n    not json\n\n"
	if out.String() != want {
		t.Errorf("Request printed:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	r.Request(http.MethodPost, "/x", nil, []byte(`{"a":1}`))
	if !strings.Contains(out.String(), "    Content-Type: application/json\n\n    {\n      \"a\": 1\n    }\n") {
		t.Errorf("Request printed:\n%s", out.String())
	}
}

func TestReporter_ResponsePrintsStatusBodyThenChosenHeaders(t *testing.T) {
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
	r.Response(res)
	want := "\n" +
		"  HTTP 201 Created\n" +
		"\n" +
		"    {\n" +
		"      \"id\": \"1\",\n" +
		"      \"code\": \"acme\"\n" +
		"    }\n" +
		"\n" +
		"    Content-Type: application/json\n" +
		"    Location: /organizations/1\n" +
		"\n"
	if out.String() != want {
		t.Errorf("Response printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ResponseWithNoBodyOrHeadersPrintsTheStatusAlone(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Response(&httpx.Response{Status: http.StatusNoContent, Header: http.Header{"Date": {"never"}}})
	if want := "\n  HTTP 204 No Content\n\n"; out.String() != want {
		t.Errorf("Response printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ResponsePrintsANonJSONBodyAsIs(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Response(&httpx.Response{Status: 200, Body: []byte("not json")})
	if !strings.Contains(out.String(), "    not json\n") {
		t.Errorf("Response printed:\n%s", out.String())
	}
}

func TestReporter_TracePrintsThreeAlignedLinesAndNoQuery(t *testing.T) {
	var out bytes.Buffer
	r := NewReporter(&out, false)
	r.Trace("http://localhost:3000/", "go-web-service", "4bf92f3577b34da6a3ce929d0e0e4736")
	want := "\n" +
		"  Observability\n" +
		"    Grafana     : http://localhost:3000/explore\n" +
		"    Service Name: go-web-service\n" +
		"    Trace ID    : 4bf92f3577b34da6a3ce929d0e0e4736\n" +
		"\n"
	if out.String() != want {
		t.Errorf("Trace printed:\n%s\nwant:\n%s", out.String(), want)
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
	r.Request(http.MethodPost, "/x", []httpx.Header{{Name: "If-Match", Value: `"1"`}}, map[string]int{"n": 1})
	r.Response(&httpx.Response{Status: 200, Header: http.Header{"Location": {"/x/1"}}, Body: []byte(`{"id":"1"}`)})
	r.Trace("http://grafana", "svc", "abc")
	r.Tick("tick %d", 1)
	got := out.String()
	if !strings.Contains(got, ansiBold+ansiCyan+"do a thing"+ansiReset) {
		t.Error("Intent is not styled as a heading")
	}
	if !strings.Contains(got, ansiBold+"SELECT"+ansiReset) {
		t.Error("SQL did not bold its keyword")
	}
	for _, want := range []string{
		ansiBold + ansiYellow + "POST /x" + ansiReset,
		ansiBold + ansiYellow + "HTTP 200 OK" + ansiReset,
		ansiBold + ansiYellow + "Observability" + ansiReset,
		ansiBlue + "If-Match" + ansiReset + `: "1"`,
		ansiBlue + "Location" + ansiReset + ": /x/1",
		ansiBlue + `"n"` + ansiReset + ": " + ansiGreen + "1" + ansiReset,
		ansiBlue + `"id"` + ansiReset + ": " + ansiGreen + `"1"` + ansiReset,
		ansiBlue + "Trace ID" + ansiReset + "    : abc",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	want := "\n[1/2] do a thing\n" +
		"\n  the statement\n    SELECT 1\n" +
		"\n  POST /x\n    Content-Type: application/json\n    If-Match: \"1\"\n\n    {\n      \"n\": 1\n    }\n" +
		"\n  HTTP 200 OK\n\n    {\n      \"id\": \"1\"\n    }\n\n    Location: /x/1\n" +
		"\n  Observability\n    Grafana     : http://grafana/explore\n    Service Name: svc\n    Trace ID    : abc\n" +
		"\n    · tick 1\n"
	if strip(got) != want {
		t.Errorf("text under the escapes:\n%q\nwant:\n%q", strip(got), want)
	}
}
