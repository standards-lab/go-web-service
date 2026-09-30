package data

import (
	"context"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Directives lowers a parsed request query's sorts and filters to the
// library's read directives; the page or cursor is [Read]'s and
// [ReadListing]'s. A filter with no operator is equality for one value and
// membership for several; a bracketed operator is the library's operator
// of the same name, one filter per value (in takes every value at once).
// Values stay the request's raw text, which the library casts to the
// field's type, and the filters keep the SDK's order, so the predicate is
// deterministic.
func Directives(q web.Query) query.Directives {
	var d query.Directives
	for _, s := range q.Sort {
		d.Sort = append(d.Sort, query.Sort{Field: s.Field, Descending: s.Descending})
	}
	for _, f := range q.Filters {
		d.Filters = append(d.Filters, filters(f)...)
	}
	return d
}

func filters(f web.Filter) []query.Filter {
	op := query.Op(f.Op)
	if f.Op == "" {
		op = query.OpEq
		if len(f.Values) > 1 {
			op = query.OpIn
		}
	}
	switch op {
	case query.OpIn:
		values := make([]any, len(f.Values))
		for i, v := range f.Values {
			values[i] = v
		}
		return []query.Filter{{Field: f.Field, Op: op, Value: values}}
	case query.OpIsNull, query.OpIsNotNull:
		return []query.Filter{{Field: f.Field, Op: op}}
	}
	out := make([]query.Filter, len(f.Values))
	for i, v := range f.Values {
		out[i] = query.Filter{Field: f.Field, Op: op, Value: v}
	}
	return out
}

// Read runs a projection's collection read as the request addressed it:
// continued past a cursor a previous page returned when the query names
// one, and by page number otherwise. A cursor that did not come from this
// read, or no longer continues it, unwraps to query.ErrDirectives or is a
// *query.CursorError, both the request's error.
func Read[T any](ctx context.Context, s sqlate.Session, p query.Projection[T], q web.Query, base ...query.Args) (query.Collection[T], error) {
	d := Directives(q)
	if q.Cursor != "" {
		return p.Continue(ctx, s, d, query.Cursor(q.Cursor), q.Size, base...)
	}
	return p.List(ctx, s, d, query.Page{Number: q.Page, Size: q.Size}, base...)
}

// ReadListing is [Read] over one of blobfs's directory listings
// (bfdata.Directories and bfdata.Files, each a bfdata.Listing) in place of
// a projection: the rows under the directory with id, continued past a
// cursor when the query names one and by page number otherwise. Every
// refusal of the directives or the cursor is the request's error, as
// Read's is; what a refusal of the anchor means is the caller's policy.
func ReadListing[T any](ctx context.Context, s sqlate.Session, l bfdata.Listing[T], id string, q web.Query) (query.Collection[T], error) {
	d := Directives(q)
	if q.Cursor != "" {
		return l.Continue(ctx, s, id, d, query.Cursor(q.Cursor), q.Size)
	}
	return l.List(ctx, s, id, d, query.Page{Number: q.Page, Size: q.Size})
}

// Paging reports a collection read's outcome in the envelope's terms: the
// total (nil for query.NoTotal, a read that did not count), whether a
// further page exists, and the cursor to it.
func Paging[T any](c query.Collection[T]) web.Paging {
	p := web.Paging{More: c.More, Next: string(c.Next)}
	if c.Total != query.NoTotal {
		p.Total = new(c.Total)
	}
	return p
}
