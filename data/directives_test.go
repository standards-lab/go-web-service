package data_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
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
			want: query.Directives{Sort: []query.Sort{{Field: "name", Descending: true}}},
		},
		{
			name: "one plain value is equality",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Values: []string{"acme"}}}},
			want: query.Directives{Filters: []query.Filter{{Field: "code", Op: query.OpEq, Value: "acme"}}},
		},
		{
			name: "several plain values are membership",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Values: []string{"a", "b"}}}},
			want: query.Directives{Filters: []query.Filter{{Field: "code", Op: query.OpIn, Value: []any{"a", "b"}}}},
		},
		{
			name: "a bracketed operator conjoins per value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "created_at", Op: "ge", Values: []string{"2026-01-01", "2026-02-01"}}}},
			want: query.Directives{Filters: []query.Filter{
				{Field: "created_at", Op: query.OpGe, Value: "2026-01-01"},
				{Field: "created_at", Op: query.OpGe, Value: "2026-02-01"},
			}},
		},
		{
			name: "in takes every value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Op: "in", Values: []string{"a", "b"}}}},
			want: query.Directives{Filters: []query.Filter{{Field: "code", Op: query.OpIn, Value: []any{"a", "b"}}}},
		},
		{
			name: "null takes no value",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "parent_id", Op: "null", Values: []string{""}}}},
			want: query.Directives{Filters: []query.Filter{{Field: "parent_id", Op: query.OpIsNull}}},
		},
		{
			name: "an unknown operator passes through for the library to reject",
			in:   web.Query{Page: 1, Size: 20, Filters: []web.Filter{{Field: "code", Op: "between", Values: []string{"a"}}}},
			want: query.Directives{Filters: []query.Filter{{Field: "code", Op: "between", Value: "a"}}},
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

func TestPaging_CarriesTheCollectionsReport(t *testing.T) {
	c := query.Collection[string]{Items: []string{"a"}, Total: query.NoTotal, More: true, Next: "c2"}

	want := web.Paging{Total: web.NoTotal, More: true, Next: "c2"}
	if got := data.Paging(c); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// fakeListing records the read ReadListing chose: the anchor, the
// directives, and the page or the cursor with its size.
type fakeListing struct {
	call   string
	id     string
	d      query.Directives
	page   query.Page
	cursor query.Cursor
	size   int
	err    error
}

func (f *fakeListing) List(_ context.Context, _ sqlate.Session, id string, d query.Directives, page query.Page, _ ...bfdata.ListOption) (query.Collection[string], error) {
	f.call, f.id, f.d, f.page = "list", id, d, page
	return query.Collection[string]{Items: []string{"a"}, Total: 1}, f.err
}

func (f *fakeListing) Continue(_ context.Context, _ sqlate.Session, id string, d query.Directives, after query.Cursor, size int, _ ...bfdata.ListOption) (query.Collection[string], error) {
	f.call, f.id, f.d, f.cursor, f.size = "continue", id, d, after, size
	return query.Collection[string]{Items: []string{"b"}, Total: query.NoTotal, More: true, Next: "c3"}, f.err
}

var _ bfdata.Listing[string] = (*fakeListing)(nil)

func TestReadListing_PagesByNumberOrContinuesPastACursor(t *testing.T) {
	sort := []web.Sort{{Field: "name", Descending: true}}
	want := query.Directives{Sort: []query.Sort{{Field: "name", Descending: true}}}

	var paged fakeListing
	c, err := data.ReadListing(context.Background(), nil, &paged, "dir", web.Query{Page: 2, Size: 10, Sort: sort})
	if err != nil || paged.call != "list" || paged.id != "dir" || paged.page != (query.Page{Number: 2, Size: 10}) || !reflect.DeepEqual(paged.d, want) {
		t.Fatalf("paged read = %+v, %v", paged, err)
	}
	if c.Items[0] != "a" || c.Total != 1 {
		t.Fatalf("paged collection = %+v", c)
	}

	var continued fakeListing
	c, err = data.ReadListing(context.Background(), nil, &continued, "dir", web.Query{Page: 1, Size: 10, Cursor: "c2", Sort: sort})
	if err != nil || continued.call != "continue" || continued.id != "dir" || continued.cursor != "c2" || continued.size != 10 || !reflect.DeepEqual(continued.d, want) {
		t.Fatalf("continued read = %+v, %v", continued, err)
	}
	if c.Items[0] != "b" || !c.More || c.Next != "c3" {
		t.Fatalf("continued collection = %+v", c)
	}
}

// A refusal is the listing's own, returned unchanged for the caller's
// policy to read.
func TestReadListing_ReturnsTheListingsRefusal(t *testing.T) {
	refused := errors.New("refused")
	l := fakeListing{err: refused}
	if _, err := data.ReadListing(context.Background(), nil, &l, "dir", web.Query{Page: 1, Size: 10}); !errors.Is(err, refused) {
		t.Fatalf("err = %v; want the listing's refusal", err)
	}
}
