package data

import (
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/query"
)

// Directives lowers a parsed request query to the library's read
// directives. It lives here because go-web-sdk cannot import sqlate and
// the lowering is too small for a package of its own. The page and the
// sorts pass through as given. A filter with no operator is equality when
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
	d := query.Directives{Page: query.Page{Number: q.Page, Size: q.Size}}
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
