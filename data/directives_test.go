package data_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/query"

	"github.com/standards-lab/go-web-service/data"
)

func TestDirectives(t *testing.T) {
	cases := []struct {
		name string
		in   web.Query
		want query.Directives
	}{
		{
			name: "page and sort pass through",
			in:   web.Query{Page: 2, Size: 10, Sort: []web.Sort{{Field: "name", Descending: true}}},
			want: query.Directives{Page: query.Page{Number: 2, Size: 10}, Sort: []query.Sort{{Field: "name", Descending: true}}},
		},
		{
			name: "one plain value is equality",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Values: []string{"acme"}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{{Field: "code", Op: query.OpEq, Value: "acme"}}},
		},
		{
			name: "several plain values are membership",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Values: []string{"a", "b"}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{{Field: "code", Op: query.OpIn, Value: []any{"a", "b"}}}},
		},
		{
			name: "a bracketed operator conjoins per value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "created_at", Op: "ge", Values: []string{"2026-01-01", "2026-02-01"}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{
				{Field: "created_at", Op: query.OpGe, Value: "2026-01-01"},
				{Field: "created_at", Op: query.OpGe, Value: "2026-02-01"},
			}},
		},
		{
			name: "in takes every value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Op: "in", Values: []string{"a", "b"}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{{Field: "code", Op: query.OpIn, Value: []any{"a", "b"}}}},
		},
		{
			name: "null takes no value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "parent_id", Op: "null", Values: []string{""}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{{Field: "parent_id", Op: query.OpIsNull}}},
		},
		{
			name: "an unknown operator passes through for the library to reject",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Op: "between", Values: []string{"a"}}}},
			want: query.Directives{Page: query.Page{Number: 1, Size: 20}, Filters: []query.Filter{{Field: "code", Op: "between", Value: "a"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := data.Directives(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}
