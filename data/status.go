package data

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Status is the web.ProblemMatcher over the vocabulary every domain's store
// returns:
//
//   - a rejected directive is the request's fault
//   - an absent row is not found
//   - a unique or foreign-key violation is a conflict with the current state
//   - a stale version is a failed precondition
//   - a database that is not ready or cannot be reached is a temporary outage
//
// A handler composes it after its own matcher so the domain's errors take
// precedence. Check and not-null violations stay unmatched on purpose: a
// command's validation owns those rules, so a breach is an invariant
// failure, reported as a server fault.
func Status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, query.ErrDirectives):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, sqlate.ErrUniqueViolation), errors.Is(err, sqlate.ErrForeignKeyViolation):
		return web.Problem{Status: http.StatusConflict}, true
	case errors.Is(err, query.ErrVersionMismatch):
		return web.Problem{Status: http.StatusPreconditionFailed}, true
	case errors.Is(err, database.ErrNotReady),
		errors.Is(err, database.ErrConnectionFailed),
		errors.Is(err, sqlate.ErrConnectionFailed):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}
