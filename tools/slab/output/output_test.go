package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// plain returns an Output writing both streams to the returned buffer, with
// color off.
func plain() (*output.Output, *bytes.Buffer) {
	var out bytes.Buffer
	return output.New(&out, &out, nil), &out
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
	o := output.New(&out, &out, func() bool { return true })
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
// the Output, is built. So New must not ask; each render must.
func TestNew_AsksWhetherToColorAtEachRenderNotAtConstruction(t *testing.T) {
	asked, color := 0, false
	var out bytes.Buffer
	o := output.New(&out, &out, func() bool {
		asked++
		return color
	})
	if asked != 0 {
		t.Fatalf("New asked whether to color %d times; want none until a render", asked)
	}

	o.Response(http.StatusNoContent, nil)
	if asked != 1 {
		t.Errorf("the first render asked %d times, want once", asked)
	}
	if got := out.String(); got != "204 No Content\n" {
		t.Errorf("with color off, Response wrote %q, want it plain", got)
	}

	color = true
	out.Reset()
	o.Response(http.StatusNoContent, nil)
	if asked != 2 {
		t.Errorf("the second render brought the count to %d, want 2", asked)
	}
	if want := style.New(true).Status("204 No Content") + "\n"; out.String() != want {
		t.Errorf("with color turned on after New, Response wrote %q, want %q", out.String(), want)
	}
	if got := o.Style().Bold("x"); got != style.New(true).Bold("x") {
		t.Errorf("Style() after color turned on styled x as %q, want it on", got)
	}
}

func TestNew_NeverColorsWithoutADecision(t *testing.T) {
	var out bytes.Buffer
	o := output.New(&out, &out, nil)
	o.Response(http.StatusNoContent, nil)
	if got := out.String(); got != "204 No Content\n" {
		t.Errorf("Response wrote %q, want it plain", got)
	}
}

func TestOutput_WritesResultsToStdoutAndFailuresToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := output.New(&stdout, &stderr, nil)
	o.Response(http.StatusOK, []byte(`{}`))
	o.Error(errors.New("boom"))
	if got := stdout.String(); got != "{}\n" {
		t.Errorf("stdout = %q, want the result alone", got)
	}
	if got := stderr.String(); got != "boom\n" {
		t.Errorf("stderr = %q, want the failure alone", got)
	}
}

func TestError_RendersAProblemDocumentMemberPerLine(t *testing.T) {
	o, out := plain()
	o.Error(web.Problem{
		Type:     "https://example.test/problems/stale",
		Title:    "Precondition Failed",
		Status:   http.StatusPreconditionFailed,
		Detail:   "If-Match names version 3, the row is at 4",
		Instance: "/api/organizations/a1",
		Extras:   map[string]any{"request_id": "abc", "attempt": float64(2), "hint": map[string]any{"fetch": true}},
	})
	want := "412 Precondition Failed\n" +
		"detail: If-Match names version 3, the row is at 4\n" +
		"instance: /api/organizations/a1\n" +
		"type: https://example.test/problems/stale\n" +
		"attempt: 2\n" +
		"hint: {\"fetch\":true}\n" +
		"request_id: abc\n"
	if out.String() != want {
		t.Errorf("Error wrote:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestError_OmitsAbsentMembersAndTheBlankType(t *testing.T) {
	o, out := plain()
	o.Error(web.Problem{
		Type: web.ProblemTypeBlank, Title: "Not Found", Status: http.StatusNotFound,
	})
	if got := out.String(); got != "404 Not Found\n" {
		t.Errorf("Error wrote %q, want the status line alone", got)
	}
}

func TestError_TitlesAnUntitledProblemWithTheStatusPhrase(t *testing.T) {
	o, out := plain()
	o.Error(web.Problem{Status: http.StatusConflict, Detail: "duplicate code"})
	if got := out.String(); got != "409 Conflict\ndetail: duplicate code\n" {
		t.Errorf("Error wrote %q", got)
	}
}

func TestError_FindsAWrappedProblem(t *testing.T) {
	o, out := plain()
	p := web.Problem{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "page = 0"}
	o.Error(fmt.Errorf("org list: %w", p))
	if got := out.String(); got != "400 Bad Request\ndetail: page = 0\n" {
		t.Errorf("Error wrote %q, want the wrapped document rendered", got)
	}
}

func TestError_WritesAPlainErrorAsItsMessage(t *testing.T) {
	o, out := plain()
	o.Error(errors.New(`GET /api/organizations: dial tcp 127.0.0.1:8080: connection refused`))
	if got := out.String(); got != "GET /api/organizations: dial tcp 127.0.0.1:8080: connection refused\n" {
		t.Errorf("Error wrote %q", got)
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
		var p web.Problem
		if errors.As(err, &p) {
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
