package output_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// render runs r over a fresh buffer and returns what it wrote.
func render(t *testing.T, r output.Render) string {
	t.Helper()
	var buf bytes.Buffer
	if err := r(&buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSQL_WritesTheStatementAsAuthoredEndingAtItsLastLine(t *testing.T) {
	got := render(t, output.SQL(style.New(false), "SELECT id\nFROM t\n\n"))
	if want := "SELECT id\nFROM t\n"; got != want {
		t.Errorf("SQL wrote %q, want %q", got, want)
	}
}

func TestSQL_BoldsTheKeywordsWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.SQL(on, "SELECT id FROM t"))
	if want := on.SQL("SELECT id FROM t") + "\n"; got != want {
		t.Errorf("SQL wrote %q, want %q", got, want)
	}
}

func TestTable_AlignsTheValuesPastTheLongestName(t *testing.T) {
	got := render(t, output.Table(style.New(false), [][2]string{{"$1", "parent_id"}, {"$10", "code"}}))
	if want := "$1   parent_id\n$10  code\n"; got != want {
		t.Errorf("Table wrote %q, want %q", got, want)
	}
}

func TestTable_KeysTheNamesWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.Table(on, [][2]string{{"$1", "parent_id"}}))
	if want := on.Key("$1") + "  parent_id\n"; got != want {
		t.Errorf("Table wrote %q, want %q", got, want)
	}
}

func TestJSON_WritesTheDocumentAsAuthored(t *testing.T) {
	got := render(t, output.JSON(style.New(false), []byte("\n{\"a\": [1,\n  2]}\n\n")))
	if want := "{\"a\": [1,\n  2]}\n"; got != want {
		t.Errorf("JSON wrote %q, want %q", got, want)
	}
}

func TestJSON_ColorsTheDocumentWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.JSON(on, []byte(`{"a": "b"}`)))
	if want := on.JSON(`{"a": "b"}`) + "\n"; got != want {
		t.Errorf("JSON wrote %q, want %q", got, want)
	}
}

func TestRequest_ShowsWhatTheClientSends(t *testing.T) {
	off := style.New(false)
	for name, tc := range map[string]struct {
		headers []httpx.Header
		body    any
		want    string
	}{
		"no body": {
			nil, nil,
			"GET /api/organizations\n",
		},
		"a value encoded as JSON, with the Content-Type the client adds": {
			[]httpx.Header{httpx.IfMatch(3)}, map[string]string{"code": "sales"},
			"PUT /p\nContent-Type: application/json\nIf-Match: \"3\"\n\n{\n  \"code\": \"sales\"\n}\n",
		},
		"a JSON string, indented": {
			nil, `{"a":1}`,
			"PUT /p\nContent-Type: application/json\n\n{\n  \"a\": 1\n}\n",
		},
		"a string that is no JSON, as it is": {
			[]httpx.Header{{Name: "Content-Type", Value: "text/plain"}}, "notes\n",
			"PUT /p\nContent-Type: text/plain\n\nnotes\n",
		},
		"bytes, under the caller's own Content-Type": {
			[]httpx.Header{{Name: "content-type", Value: "image/png"}}, []byte("(4 bytes)"),
			"PUT /p\ncontent-type: image/png\n\n(4 bytes)\n",
		},
		"a body that does not encode, with the reason": {
			nil, func() {},
			"PUT /p\nContent-Type: application/json\n\n(body does not encode: json: unsupported type: func())\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			method, path := "PUT", "/p"
			if tc.body == nil {
				method, path = "GET", "/api/organizations"
			}
			if got := render(t, output.Request(off, method, path, tc.headers, tc.body)); got != tc.want {
				t.Errorf("Request wrote %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequest_ColorsTheLineTheHeadersAndTheBodyWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.Request(on, "POST", "/p", nil, map[string]int{"a": 1}))
	want := on.Status("POST /p") + "\n" + on.Key("Content-Type") + ": application/json\n\n" + on.JSON("{\n  \"a\": 1\n}") + "\n"
	if got != want {
		t.Errorf("Request wrote %q, want %q", got, want)
	}
}

func TestResponse_ShowsTheStatusTheBodyAndTheShownHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("X-Request-Id", "abc")
	header.Set("Content-Type", "application/json")
	header.Set("Date", "today")
	res := &httpx.Response{Status: http.StatusCreated, Header: header, Body: []byte(`{"id":"x"}` + "\n")}
	got := render(t, output.Response(style.New(false), res))
	want := "HTTP 201 Created\n\n{\n  \"id\": \"x\"\n}\n\nContent-Type: application/json\nX-Request-Id: abc\n"
	if got != want {
		t.Errorf("Response wrote %q, want %q", got, want)
	}
}

func TestResponse_ShowsABodilessResponseAsItsStatusLine(t *testing.T) {
	got := render(t, output.Response(style.New(false), &httpx.Response{Status: http.StatusNoContent, Header: http.Header{}}))
	if want := "HTTP 204 No Content\n"; got != want {
		t.Errorf("Response wrote %q, want %q", got, want)
	}
}

func TestResponse_ShowsABodyThatIsNoJSONAsItCame(t *testing.T) {
	res := &httpx.Response{Status: http.StatusOK, Header: http.Header{}, Body: []byte("(9 bytes of image/png)")}
	got := render(t, output.Response(style.New(false), res))
	if want := "HTTP 200 OK\n\n(9 bytes of image/png)\n"; got != want {
		t.Errorf("Response wrote %q, want %q", got, want)
	}
}

func TestResponse_ColorsTheStatusWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.Response(on, &httpx.Response{Status: http.StatusOK, Header: http.Header{}}))
	if want := on.Status("HTTP 200 OK") + "\n"; got != want {
		t.Errorf("Response wrote %q, want %q", got, want)
	}
}

func TestTrace_PointsAtTheExplorePageTheServiceAndTheTrace(t *testing.T) {
	got := render(t, output.Trace(style.New(false), "http://g:3000/", "svc", "4bf9"))
	want := "Grafana     : http://g:3000/explore\nService Name: svc\nTrace ID    : 4bf9\n"
	if got != want {
		t.Errorf("Trace wrote %q, want %q", got, want)
	}
}

func TestTrace_KeysTheNamesWhenColorIsOn(t *testing.T) {
	on := style.New(true)
	got := render(t, output.Trace(on, "http://g", "svc", "4bf9"))
	if !strings.HasPrefix(got, on.Key("Grafana")+"     : http://g/explore\n") {
		t.Errorf("Trace wrote %q, want the names keyed", got)
	}
}
