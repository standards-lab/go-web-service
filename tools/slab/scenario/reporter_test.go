package scenario_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/scenario"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return ansi.ReplaceAllString(s, "") }

func TestReporter_RequestPrintsLineHeadersAndEncodedBody(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
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
	r := scenario.NewReporter(&out, false)
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
	r := scenario.NewReporter(&out, false)
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
	r := scenario.NewReporter(&out, false)
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

func TestReporter_ResponsePrintsAnObjectsValidators(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.Response(&httpx.Response{
		Status: http.StatusNotModified,
		Header: http.Header{
			"Etag":          {`"0x1"`},
			"Last-Modified": {"Mon, 28 Sep 2026 16:00:00 GMT"},
			"Cache-Control": {"no-cache"},
		},
	})
	want := "\n" +
		"  HTTP 304 Not Modified\n" +
		"\n" +
		"    ETag: \"0x1\"\n" +
		"    Last-Modified: Mon, 28 Sep 2026 16:00:00 GMT\n" +
		"\n"
	if out.String() != want {
		t.Errorf("Response printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ResponseWithNoBodyOrHeadersPrintsTheStatusAlone(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.Response(&httpx.Response{Status: http.StatusNoContent, Header: http.Header{"Date": {"never"}}})
	if want := "\n  HTTP 204 No Content\n\n"; out.String() != want {
		t.Errorf("Response printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ResponsePrintsANonJSONBodyAsIs(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.Response(&httpx.Response{Status: 200, Body: []byte("not json")})
	if !strings.Contains(out.String(), "    not json\n") {
		t.Errorf("Response printed:\n%s", out.String())
	}
}

func TestReporter_TracePrintsThreeAlignedLinesAndNoQuery(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
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
	r := scenario.NewReporter(&out, false)
	r.Table("fields", [][2]string{{"id", "1"}, {"parent_id", "none"}})
	want := "  fields\n    id         1\n    parent_id  none\n"
	if out.String() != want {
		t.Errorf("Table printed:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestReporter_ColorOnWrapsEveryChannel(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, true)
	r.Intent(1, 2, "do a thing")
	r.SQL("the statement", "SELECT 1")
	r.Request(http.MethodPost, "/x", []httpx.Header{{Name: "If-Match", Value: `"1"`}}, map[string]int{"n": 1})
	r.Response(&httpx.Response{Status: 200, Header: http.Header{"Location": {"/x/1"}}, Body: []byte(`{"id":"1"}`)})
	r.Trace("http://grafana", "svc", "abc")
	r.Tick("tick %d", 1)
	got := out.String()

	on := style.New(true)
	if !strings.Contains(got, on.Heading("do a thing")) {
		t.Error("Intent is not styled as a heading")
	}
	if !strings.Contains(got, on.Bold("SELECT")) {
		t.Error("SQL did not bold its keyword")
	}
	for _, want := range []string{
		on.Status("POST /x"),
		on.Status("HTTP 200 OK"),
		on.Status("Observability"),
		on.Key("If-Match") + `: "1"`,
		on.Key("Location") + ": /x/1",
		on.Key(`"n"`) + ": " + on.Value("1"),
		on.Key(`"id"`) + ": " + on.Value(`"1"`),
		on.Key("Trace ID") + "    : abc",
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

func TestReporter_JSONPrintsACaptionedColoredBlockAsAuthored(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.JSON("data/seeds/default.json", []byte("{\"organizations\": [{\"code\": \"acme\"}]}\n"))
	want := "\n" +
		"  data/seeds/default.json\n" +
		"    {\"organizations\": [{\"code\": \"acme\"}]}\n" +
		"\n"
	if out.String() != want {
		t.Errorf("JSON printed:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	r2 := scenario.NewReporter(&out, true)
	r2.JSON("caption", []byte(`{"code": "acme"}`))
	got := out.String()
	st := style.New(true)
	if !strings.Contains(got, st.Caption("caption")) {
		t.Error("the caption is not styled as a caption")
	}
	if !strings.Contains(got, st.Key(`"code"`)+": "+st.Value(`"acme"`)) {
		t.Errorf("the body is not colored as JSON:\n%q", got)
	}
}

func TestReporter_JSONPrintsANonJSONDocumentAsIs(t *testing.T) {
	var out bytes.Buffer
	r := scenario.NewReporter(&out, false)
	r.JSON("a file", []byte("not json"))
	if want := "\n  a file\n    not json\n\n"; out.String() != want {
		t.Errorf("JSON printed %q, want %q", out.String(), want)
	}
}
