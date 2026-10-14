package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// plain returns an Output writing to the returned buffer, with color off.
func plain() (*output.Output, *bytes.Buffer) {
	var out bytes.Buffer
	return output.New(&out, nil), &out
}

func TestResponse_PrettyPrintsANonEmptyBody(t *testing.T) {
	o, out := plain()
	o.Response(http.StatusOK, []byte(`{"id":"a1","items":[1,2],"nested":{"k":"v"}}`))
	want := "{\n" +
		"  \"id\": \"a1\",\n" +
		"  \"items\": [\n" +
		"    1,\n" +
		"    2\n" +
		"  ],\n" +
		"  \"nested\": {\n" +
		"    \"k\": \"v\"\n" +
		"  }\n" +
		"}\n"
	if out.String() != want {
		t.Errorf("Response wrote:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestResponse_WritesTheStatusLineForAnEmptyBody(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusNoContent: "204 No Content\n",
		http.StatusOK:        "200 OK\n",
		http.StatusCreated:   "201 Created\n",
		http.StatusAccepted:  "202 Accepted\n",
	} {
		for _, body := range [][]byte{nil, {}, []byte("  \n")} {
			o, out := plain()
			o.Response(status, body)
			if out.String() != want {
				t.Errorf("Response(%d, %q) wrote %q, want %q", status, body, out.String(), want)
			}
		}
	}
}

func TestStatus_WritesTheStatusLineThenEachLine(t *testing.T) {
	o, out := plain()
	o.Status(http.StatusAccepted, "Location: /x/1", "being deleted")
	if want := "202 Accepted\nLocation: /x/1\nbeing deleted\n"; out.String() != want {
		t.Errorf("Status wrote %q, want %q", out.String(), want)
	}
}

// Block writes the Render to stdout and returns its error.
func TestBlock_WritesTheRenderToStdout(t *testing.T) {
	o, out := plain()
	if err := o.Block(output.Table(style.New(false), [][2]string{{"id", "e1"}})); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if want := "id  e1\n"; out.String() != want {
		t.Errorf("Block wrote %q, want %q", out.String(), want)
	}
	failed := errors.New("failed")
	if err := o.Block(func(io.Writer) error { return failed }); !errors.Is(err, failed) {
		t.Errorf("Block = %v, want the Render's error", err)
	}
}

// Object shows the headers it knows in its own order, whatever order the
// response carried them in, and skips the ones the response lacks and any
// it does not know.
func TestObject_WritesTheStatusTheKnownHeadersAndTheSavedFile(t *testing.T) {
	o, out := plain()
	header := http.Header{}
	header.Set("ETag", `"e1"`)
	header.Set("Content-Disposition", `attachment; filename="a.txt"`)
	header.Set("Content-Type", "text/plain")
	header.Set("X-Other", "unshown")
	o.Object(http.StatusOK, header, 3, "/tmp/a.txt")
	want := "200 OK\n" +
		"Content-Type: text/plain\n" +
		"Content-Disposition: attachment; filename=\"a.txt\"\n" +
		"ETag: \"e1\"\n" +
		"3 bytes written to /tmp/a.txt\n"
	if out.String() != want {
		t.Errorf("Object wrote:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestResponse_WritesANonJSONBodyAsIs(t *testing.T) {
	o, out := plain()
	o.Response(http.StatusOK, []byte("plain text"))
	if got := out.String(); got != "plain text\n" {
		t.Errorf("Response wrote %q, want the body and a newline", got)
	}
}

func TestResponse_ColorsTheBodyAndStatusLineWhenOn(t *testing.T) {
	on := style.New(true)
	var out bytes.Buffer
	o := output.New(&out, func() bool { return true })
	o.Response(http.StatusOK, []byte(`{"code":"acme"}`))
	if want := on.Key(`"code"`) + ": " + on.Value(`"acme"`); !strings.Contains(out.String(), want) {
		t.Errorf("Response wrote %q, want it colored as JSON", out.String())
	}

	out.Reset()
	o.Response(http.StatusNoContent, nil)
	if want := on.Status("204 No Content") + "\n"; out.String() != want {
		t.Errorf("Response wrote %q, want %q", out.String(), want)
	}
}

// Whether to color is decided by a flag parsed after the tree, and so after
// the Output, is built. So the decision New is given must be read at each
// render: one turned on after New colors the next render.
func TestNew_DecidesColorAtEachRenderNotAtConstruction(t *testing.T) {
	color := false
	var out bytes.Buffer
	o := output.New(&out, func() bool { return color })

	o.Response(http.StatusNoContent, nil)
	if got := out.String(); got != "204 No Content\n" {
		t.Errorf("with color off, Response wrote %q, want it plain", got)
	}

	color = true
	out.Reset()
	o.Response(http.StatusNoContent, nil)
	if want := style.New(true).Status("204 No Content") + "\n"; out.String() != want {
		t.Errorf("with color turned on after New, Response wrote %q, want %q", out.String(), want)
	}
	if got := o.Style().Bold("x"); got != style.New(true).Bold("x") {
		t.Errorf("Style() after color turned on styled x as %q, want it on", got)
	}
}

func TestNew_NeverColorsWithoutADecision(t *testing.T) {
	var out bytes.Buffer
	o := output.New(&out, nil)
	o.Response(http.StatusNoContent, nil)
	if got := out.String(); got != "204 No Content\n" {
		t.Errorf("Response wrote %q, want it plain", got)
	}
}

func problemResponse(status int, body string) *httpx.Response {
	h := http.Header{}
	h.Set("Content-Type", web.ProblemMediaType)
	return &httpx.Response{Status: status, Header: h, Body: []byte(body)}
}

func TestExpect_IsNilWhenTheStatusMatches(t *testing.T) {
	res := &httpx.Response{Status: http.StatusOK, Header: http.Header{}, Body: []byte(`{}`)}
	if err := output.Expect(res, http.StatusOK); err != nil {
		t.Errorf("Expect = %v, want nil", err)
	}
}

func TestExpect_DecodesTheProblemDocumentAnUnexpectedStatusCarries(t *testing.T) {
	res := problemResponse(http.StatusNotFound, `{"type":"about:blank","title":"Not Found","status":404,"instance":"/api/organizations/x","request_id":"r1"}`)
	err := output.Expect(res, http.StatusOK)
	var p web.Problem
	if !errors.As(err, &p) {
		t.Fatalf("Expect = %v (%T), want a web.Problem", err, err)
	}
	want := web.Problem{Type: "about:blank", Title: "Not Found", Status: 404, Instance: "/api/organizations/x", Extras: map[string]any{"request_id": "r1"}}
	if p.Type != want.Type || p.Title != want.Title || p.Status != want.Status ||
		p.Instance != want.Instance || p.Extras["request_id"] != "r1" {
		t.Errorf("Expect decoded %+v, want %+v", p, want)
	}
}

func TestExpect_FallsBackToTheStatusAndBodyForANonProblemResponse(t *testing.T) {
	for name, res := range map[string]*httpx.Response{
		"html":               {Status: http.StatusBadGateway, Header: http.Header{"Content-Type": {"text/html"}}, Body: []byte("<h1>502</h1>")},
		"malformed problem":  problemResponse(http.StatusInternalServerError, `{"status":`),
		"disagreeing status": problemResponse(http.StatusInternalServerError, `{"status":400,"title":"Bad Request"}`),
	} {
		err := output.Expect(res, http.StatusOK)
		if _, ok := errors.AsType[web.Problem](err); ok {
			t.Errorf("%s: Expect = %v, want a plain error", name, err)
			continue
		}
		if err == nil {
			t.Errorf("%s: Expect = nil, want an error", name)
			continue
		}
		want := fmt.Sprintf("status = %d %s, want 200; body: %s", res.Status, http.StatusText(res.Status), res.Body)
		if err.Error() != want {
			t.Errorf("%s: Expect = %q, want %q", name, err.Error(), want)
		}
	}
}
