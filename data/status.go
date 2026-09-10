package data

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// Status is the status matcher over the vocabulary every domain's store
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
func Status(err error) (int, bool) {
	switch {
	case errors.Is(err, query.ErrDirectives):
		return http.StatusBadRequest, true
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound, true
	case errors.Is(err, sqlate.ErrUniqueViolation), errors.Is(err, sqlate.ErrForeignKeyViolation):
		return http.StatusConflict, true
	case errors.Is(err, query.ErrVersionMismatch):
		return http.StatusPreconditionFailed, true
	case errors.Is(err, database.ErrNotReady),
		errors.Is(err, database.ErrConnectionFailed),
		errors.Is(err, sqlate.ErrConnectionFailed):
		return http.StatusServiceUnavailable, true
	}
	return 0, false
}
