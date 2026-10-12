package output_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

func TestReport_RendersEveryMemberOnItsOwnLine(t *testing.T) {
	r := output.Report{Problem: web.Problem{
		Type:     "https://example.test/problems/stale",
		Title:    "Precondition Failed",
		Status:   http.StatusPreconditionFailed,
		Detail:   "If-Match names version 3, the row is at 4",
		Instance: "/api/organizations/a1",
		Extras:   map[string]any{"request_id": "abc", "attempt": float64(2), "hint": map[string]any{"fetch": true}},
	}}
	want := "412 Precondition Failed\n" +
		"detail: If-Match names version 3, the row is at 4\n" +
		"instance: /api/organizations/a1\n" +
		"type: https://example.test/problems/stale\n" +
		"attempt: 2\n" +
		"hint: {\"fetch\":true}\n" +
		"request_id: abc"
	if got := r.Error(); got != want {
		t.Errorf("Error() =\n%s\nwant:\n%s", got, want)
	}
}

func TestReport_OmitsAbsentMembersAndTheBlankType(t *testing.T) {
	r := output.Report{Problem: web.Problem{Type: web.ProblemTypeBlank, Status: http.StatusNotFound}}
	if got := r.Error(); got != "404 Not Found" {
		t.Errorf("Error() = %q, want the status line alone", got)
	}
}

// errors.As finds the document through a Report, however it is wrapped.
func TestReport_UnwrapsToTheProblem(t *testing.T) {
	err := fmt.Errorf("context: %w", output.Report{Problem: web.Problem{Status: http.StatusConflict, Detail: "duplicate"}})
	var p web.Problem
	if !errors.As(err, &p) || p.Status != http.StatusConflict || p.Detail != "duplicate" {
		t.Errorf("errors.As(%v) = %+v, want the problem", err, p)
	}
}

func TestExpect_ReturnsTheReportOfAProblemResponse(t *testing.T) {
	res := &httpx.Response{
		Status: http.StatusNotFound,
		Header: http.Header{"Content-Type": {web.ProblemMediaType}},
		Body:   []byte(`{"status":404,"title":"Not Found","detail":"no row","instance":"/x"}`),
	}
	err := output.Expect(res, http.StatusOK)
	var r output.Report
	if !errors.As(err, &r) {
		t.Fatalf("err = %v (%T), want an output.Report", err, err)
	}
	if want := "404 Not Found\ndetail: no row\ninstance: /x"; r.Error() != want {
		t.Errorf("report = %q, want %q", r.Error(), want)
	}
}
