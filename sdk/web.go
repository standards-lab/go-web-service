package sdk

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"

	"github.com/standards-lab/go-database/query"
	"github.com/standards-lab/go-web-sdk"
)

// reserved names the read contract's directive parameters — the ones
// [web.ParseDirectives] consumes. No other layer knows this set.
var reserved = map[string]struct{}{
	"page": {},
	"size": {},
	"sort": {},
}

// ParseRead parses one read request's query string in full: the page, size,
// and sort directives under the consumer's limits, and every remaining
// parameter as the filter set. One call yields both halves, so a handler
// cannot parse the directives and forget to strip them from the filters. A
// malformed directive is a *[web.DirectiveError]; filter names are lexical
// here — validating them against the read model is the data layer's job.
func ParseRead(q url.Values, l web.Limits) (web.Directives, url.Values, error) {
	d, err := web.ParseDirectives(q, l)
	if err != nil {
		return web.Directives{}, nil, err
	}
	filters := url.Values{}
	for key, values := range q {
		if _, ok := reserved[key]; ok {
			continue
		}
		filters[key] = values
	}
	return d, filters, nil
}

// Status maps a read-path error to its HTTP status: the request-shaped
// rejections — a directive error, an unknown field or operator — to 400, a
// missing row to 404, anything else to 500.
func Status(err error) int {
	var directive *web.DirectiveError
	var field *query.UnknownFieldError
	var operator *query.UnknownOperatorError
	switch {
	case errors.As(err, &directive),
		errors.As(err, &field),
		errors.As(err, &operator):
		return http.StatusBadRequest
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// WriteError writes err as an RFC 9457 problem at [Status]'s mapping. The
// detail carries the error text only on a 400, where it is request-shaped
// and client-actionable; every other status sends the bare title — an
// internal error's text never reaches the wire.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status := Status(err)
	detail := ""
	if status == http.StatusBadRequest {
		detail = err.Error()
	}
	_ = web.WriteProblem(w, r, status, "", detail)
}
