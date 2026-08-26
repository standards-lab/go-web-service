package sdk_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/standards-lab/go-database/query"
	"github.com/standards-lab/go-web-sdk"
	servicesdk "github.com/standards-lab/go-web-service/sdk"
)

var limits = web.Limits{DefaultSize: 20, MaxSize: 100}

func TestParseRead_SeparatesDirectivesAndFilters(t *testing.T) {
	q := url.Values{
		"page": {"2"},
		"size": {"5"},
		"sort": {"-name,code"},
		"code": {"engineering"},
		"path": {"/acme"},
	}

	d, filters, err := servicesdk.ParseRead(q, limits)
	if err != nil {
		t.Fatalf("ParseRead: %v", err)
	}

	if d.Page != 2 || d.Size != 5 || len(d.Sort) != 2 {
		t.Errorf("Directives = %+v, want page 2, size 5, two sorts", d)
	}
	// The reserved directive parameters never reach the filter set; every
	// other parameter does.
	want := url.Values{"code": {"engineering"}, "path": {"/acme"}}
	if len(filters) != len(want) || filters.Get("code") != "engineering" || filters.Get("path") != "/acme" {
		t.Errorf("filters = %v, want %v", filters, want)
	}
}

func TestParseRead_DirectiveErrorPassesThrough(t *testing.T) {
	_, _, err := servicesdk.ParseRead(url.Values{"page": {"x"}}, limits)
	var de *web.DirectiveError
	if !errors.As(err, &de) {
		t.Fatalf("ParseRead error = %v, want *web.DirectiveError", err)
	}
}

func TestStatus_MapsReadErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"directive":        {&web.DirectiveError{Param: "page"}, http.StatusBadRequest},
		"unknown field":    {&query.UnknownFieldError{Field: "bogus"}, http.StatusBadRequest},
		"unknown operator": {&query.UnknownOperatorError{Op: "zz"}, http.StatusBadRequest},
		"wrapped field":    {fmt.Errorf("list: %w", &query.UnknownFieldError{Field: "x"}), http.StatusBadRequest},
		"no rows":          {sql.ErrNoRows, http.StatusNotFound},
		"wrapped no rows":  {fmt.Errorf("find: %w", sql.ErrNoRows), http.StatusNotFound},
		"anything else":    {errors.New("boom"), http.StatusInternalServerError},
	}
	for name, tc := range cases {
		if got := servicesdk.Status(tc.err); got != tc.want {
			t.Errorf("%s: Status = %d, want %d", name, got, tc.want)
		}
	}
}

func TestWriteError_DetailOnlyOnBadRequest(t *testing.T) {
	cases := map[string]struct {
		err        error
		status     int
		wantDetail bool
	}{
		"bad request carries the error text": {&web.DirectiveError{Param: "size", Value: "1000", Reason: "too big"}, http.StatusBadRequest, true},
		"not found sends the bare title":     {sql.ErrNoRows, http.StatusNotFound, false},
		"internal error leaks nothing":       {errors.New("pq: connection refused"), http.StatusInternalServerError, false},
	}

	for name, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/organizations", nil)

		servicesdk.WriteError(rec, req, tc.err)

		if rec.Code != tc.status {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, tc.status)
		}
		if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
			t.Errorf("%s: Content-Type = %q, want %q", name, ct, web.ProblemMediaType)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode body: %v", name, err)
		}
		_, hasDetail := body["detail"]
		if hasDetail != tc.wantDetail {
			t.Errorf("%s: detail present = %t, want %t (body %v)", name, hasDetail, tc.wantDetail, body)
		}
		if tc.wantDetail && body["detail"] != tc.err.Error() {
			t.Errorf("%s: detail = %q, want %q", name, body["detail"], tc.err.Error())
		}
	}
}
