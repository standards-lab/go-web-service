package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

func TestResponse_PrettyPrintsANonEmptyBody(t *testing.T) {
	var out bytes.Buffer
	output.Response(&out, http.StatusOK, []byte(`{"id":"a1","items":[1,2],"nested":{"k":"v"}}`))
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
			var out bytes.Buffer
			output.Response(&out, status, body)
			if out.String() != want {
				t.Errorf("Response(%d, %q) wrote %q, want %q", status, body, out.String(), want)
			}
		}
	}
}

func TestResponse_WritesANonJSONBodyAsIs(t *testing.T) {
	var out bytes.Buffer
	output.Response(&out, http.StatusOK, []byte("plain text"))
	if got := out.String(); got != "plain text\n" {
		t.Errorf("Response wrote %q, want the body and a newline", got)
	}
}

func TestError_RendersAProblemDocumentMemberPerLine(t *testing.T) {
	var out bytes.Buffer
	output.Error(&out, &output.ProblemError{Problem: web.Problem{
		Type:     "https://example.test/problems/stale",
		Title:    "Precondition Failed",
		Status:   http.StatusPreconditionFailed,
		Detail:   "If-Match names version 3, the row is at 4",
		Instance: "/api/organizations/a1",
		Extras:   map[string]any{"request_id": "abc", "attempt": float64(2), "hint": map[string]any{"fetch": true}},
	}})
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
	var out bytes.Buffer
	output.Error(&out, &output.ProblemError{Problem: web.Problem{
		Type: web.ProblemTypeBlank, Title: "Not Found", Status: http.StatusNotFound,
	}})
	if got := out.String(); got != "404 Not Found\n" {
		t.Errorf("Error wrote %q, want the status line alone", got)
	}
}

func TestError_TitlesAnUntitledProblemWithTheStatusPhrase(t *testing.T) {
	var out bytes.Buffer
	output.Error(&out, &output.ProblemError{Problem: web.Problem{Status: http.StatusConflict, Detail: "duplicate code"}})
	if got := out.String(); got != "409 Conflict\ndetail: duplicate code\n" {
		t.Errorf("Error wrote %q", got)
	}
}

func TestError_FindsAWrappedProblem(t *testing.T) {
	var out bytes.Buffer
	pe := &output.ProblemError{Problem: web.Problem{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "page = 0"}}
	output.Error(&out, fmt.Errorf("org list: %w", pe))
	if got := out.String(); got != "400 Bad Request\ndetail: page = 0\n" {
		t.Errorf("Error wrote %q, want the wrapped document rendered", got)
	}
}

func TestError_WritesAPlainErrorAsItsMessage(t *testing.T) {
	var out bytes.Buffer
	output.Error(&out, errors.New(`GET /api/organizations: dial tcp 127.0.0.1:8080: connection refused`))
	if got := out.String(); got != "GET /api/organizations: dial tcp 127.0.0.1:8080: connection refused\n" {
		t.Errorf("Error wrote %q", got)
	}
}

func TestProblemError_ErrorIsTheStatusTitleAndDetail(t *testing.T) {
	for _, tc := range []struct {
		p    web.Problem
		want string
	}{
		{web.Problem{Status: 404, Title: "Not Found"}, "404 Not Found"},
		{web.Problem{Status: 400, Title: "Bad Request", Detail: "page = 0"}, "400 Bad Request: page = 0"},
		{web.Problem{Status: 409}, "409 Conflict"},
	} {
		if got := (&output.ProblemError{Problem: tc.p}).Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
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
	var pe *output.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("Expect = %v (%T), want a *ProblemError", err, err)
	}
	want := web.Problem{Type: "about:blank", Title: "Not Found", Status: 404, Instance: "/api/organizations/x", Extras: map[string]any{"request_id": "r1"}}
	if pe.Problem.Type != want.Type || pe.Problem.Title != want.Title || pe.Problem.Status != want.Status ||
		pe.Problem.Instance != want.Instance || pe.Problem.Extras["request_id"] != "r1" {
		t.Errorf("Expect decoded %+v, want %+v", pe.Problem, want)
	}
}

func TestExpect_FallsBackToTheStatusAndBodyForANonProblemResponse(t *testing.T) {
	for name, res := range map[string]*httpx.Response{
		"html":               {Status: http.StatusBadGateway, Header: http.Header{"Content-Type": {"text/html"}}, Body: []byte("<h1>502</h1>")},
		"malformed problem":  problemResponse(http.StatusInternalServerError, `{"status":`),
		"disagreeing status": problemResponse(http.StatusInternalServerError, `{"status":400,"title":"Bad Request"}`),
	} {
		err := output.Expect(res, http.StatusOK)
		var pe *output.ProblemError
		if errors.As(err, &pe) {
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
