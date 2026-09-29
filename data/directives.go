package data

import (
	"context"

	bfdata "github.com/standards-lab/blobfs/data"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Directives lowers a parsed request query to the library's read
// directives. It lives here because go-web-sdk cannot import sqlate and
// the lowering is too small for a package of its own. The sorts pass
// through as given; the page or cursor the request addressed is
// [Read]'s and [ReadListing]'s. A filter with no operator is equality when
// the parameter carried one value and membership when it carried several;
// a bracketed operator maps to the library's operator of the same name,
// one filter per value, so repeated parameters conjoin, and the in
// operator takes every value at once. Every value is the request's raw
// text: the library casts it to the field's declared type, so a malformed
// value is the request's error. The filters keep the SDK's order, field
// then operator, so the composed predicate is deterministic. Whether a
// field is readable or an operator is one the read model supports is the
// library's check.
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

// Listing is a collection read anchored on one row's id, the shape of
// blobfs's directory listings (bfdata.Directories and bfdata.Files, which
// satisfy it as they are): List reads a page by number and Continue the
// page past a cursor a previous page returned, both of the rows under the
// directory with id.
type Listing[T any] interface {
	List(ctx context.Context, s sqlate.Session, id string, d query.Directives, page query.Page, opts ...bfdata.ListOption) (query.Collection[T], error)
	Continue(ctx context.Context, s sqlate.Session, id string, d query.Directives, after query.Cursor, size int, opts ...bfdata.ListOption) (query.Collection[T], error)
}

// ReadListing is [Read] over a listing in place of a projection: the rows
// under the directory with id, continued past a cursor when the query
// names one and by page number otherwise. Every refusal of the directives
// or the cursor is the request's error, as Read's is; what a refusal of
// the anchor means is the caller's policy.
func ReadListing[T any](ctx context.Context, s sqlate.Session, l Listing[T], id string, q web.Query) (query.Collection[T], error) {
	d := Directives(q)
	if q.Cursor != "" {
		return l.Continue(ctx, s, id, d, query.Cursor(q.Cursor), q.Size)
	}
	return l.List(ctx, s, id, d, query.Page{Number: q.Page, Size: q.Size})
}

// Paging reports a collection read's outcome in the envelope's terms: the
// total (query.NoTotal is the envelope's web.NoTotal), whether a further
// page exists, and the cursor to it.
func Paging[T any](c query.Collection[T]) web.Paging {
	return web.Paging{Total: c.Total, More: c.More, Next: string(c.Next)}
}
