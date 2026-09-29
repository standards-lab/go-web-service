package data

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// The curated details a conflict carries on the wire, one text per kind. A
// 409's detail is always one of these and never the error's own text,
// which names the library's operation, its ids, and its constraints.
const (
	DetailNameTaken    = "an entry with that name already exists"
	DetailNotEmpty     = "the directory is not empty"
	DetailDeleting     = "the directory is being deleted"
	DetailFileDeleting = "the file is being deleted"
	DetailReferenced   = "the file is referenced"
	// DetailConflict is every other conflict's: a constraint violation, a
	// move into its own subtree, a transition the file's status does not
	// allow, and a domain's own conflicts, which its matcher reports with
	// this text too.
	DetailConflict = "the request conflicts with the current state"
)

// Status is the web.ProblemMatcher over the vocabulary every domain's store
// returns:
//
//   - a rejected directive is the request's fault
//   - an absent row is not found
//   - a unique or foreign-key violation is a conflict with the current state
//   - a stale version is a failed precondition
//   - a database that is not ready or cannot be reached is a temporary outage,
//     a pooled connection lost in the middle of a read included: sqlate's
//     engine leaves that unexpected EOF unclassified, so it is matched here
//
// and the storage vocabulary, blobfs's sentinels and go-storage's, which
// comes first because a blobfs violation also unwraps to the constraint
// error beneath it:
//
//   - a malformed name, path, key, or id, or an operation on the root, is
//     the request's fault
//   - an absent entry or object is not found
//   - a taken name or id, a non-empty directory, a referenced file, a
//     deleting file or directory, a move into its own subtree, or a
//     transition the file's status does not allow is a conflict (a
//     domain's listing reports a deleting directory as not found before
//     its error reaches this matcher)
//   - an object over the store's size bound is too large
//   - a store that is not ready, unreachable, or missing its container
//     (storage.ErrContainerNotFound, on every object operation, a
//     download's open included) is a temporary outage, never not found
//
// Every conflict carries a curated detail, the Detail constant for its
// kind, so a writer that opts 409 into error text still sends none of it:
// a taken name or id is DetailNameTaken, a non-empty directory
// DetailNotEmpty, a file refused by its own delete (a
// blobfs.DeletingError whose Directory is false) DetailFileDeleting, any
// other blobfs.ErrDeleting, a deleting directory's, DetailDeleting, a
// referenced file DetailReferenced, and every other conflict
// DetailConflict.
//
// A 400's detail is the refusal alone, the input the request got wrong:
// never the library's operation chain, which names the service's own ids,
// such as a directory a root alias resolved to (requestDetail).
//
// A handler composes it after its own matcher so the domain's errors take
// precedence. Check and not-null violations stay unmatched on purpose: a
// command's validation owns those rules, so a breach is an invariant
// failure, reported as a server fault.
func Status(err error) (web.Problem, bool) {
	var deleting *blobfs.DeletingError
	switch {
	case errors.Is(err, blobfs.ErrInvalidName), errors.Is(err, blobfs.ErrInvalidPath),
		errors.Is(err, blobfs.ErrInvalidKey), errors.Is(err, blobfs.ErrInvalidID),
		errors.Is(err, blobfs.ErrRootDirectory):
		return web.Problem{Status: http.StatusBadRequest, Detail: requestDetail(err)}, true
	case errors.Is(err, blobfs.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, blobfs.ErrNameTaken), errors.Is(err, blobfs.ErrIDTaken):
		return Conflict(DetailNameTaken), true
	case errors.Is(err, blobfs.ErrNotEmpty):
		return Conflict(DetailNotEmpty), true
	case errors.Is(err, blobfs.ErrReferenced):
		return Conflict(DetailReferenced), true
	case errors.As(err, &deleting) && !deleting.Directory:
		return Conflict(DetailFileDeleting), true
	case errors.Is(err, blobfs.ErrDeleting):
		return Conflict(DetailDeleting), true
	case errors.Is(err, blobfs.ErrNotDeleting),
		errors.Is(err, blobfs.ErrInvalidTransition), errors.Is(err, blobfs.ErrCycle):
		return Conflict(DetailConflict), true
	case errors.Is(err, storage.ErrTooLarge):
		return web.Problem{Status: http.StatusRequestEntityTooLarge}, true
	case errors.Is(err, storage.ErrContainerNotFound), errors.Is(err, storage.ErrNotReady), errors.Is(err, storage.ErrUnavailable):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	case errors.Is(err, query.ErrDirectives):
		return web.Problem{Status: http.StatusBadRequest, Detail: requestDetail(err)}, true
	case errors.Is(err, sql.ErrNoRows):
		return web.Problem{Status: http.StatusNotFound}, true
	case errors.Is(err, sqlate.ErrUniqueViolation), errors.Is(err, sqlate.ErrForeignKeyViolation):
		return Conflict(DetailConflict), true
	case errors.Is(err, query.ErrVersionMismatch):
		return web.Problem{Status: http.StatusPreconditionFailed}, true
	case errors.Is(err, database.ErrNotReady),
		errors.Is(err, database.ErrConnectionFailed),
		errors.Is(err, sqlate.ErrConnectionFailed),
		errors.Is(err, io.ErrUnexpectedEOF):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}

// requestDetail is the detail of a 400: the text of the typed refusal the
// libraries report, the one that names the request's own input (a name,
// an id, a sort or filter field, a filter value, a cursor), without its
// library prefix or the operation chain that wrapped it. A refusal with no
// typed error is its sentinel's text.
func requestDetail(err error) string {
	var (
		name     *blobfs.NameError
		id       *blobfs.IDError
		cursor   *query.CursorError
		field    *query.UnknownFieldError
		operator *query.UnknownOperatorError
		value    *query.InvalidValueError
	)
	var text string
	switch {
	case errors.As(err, &cursor):
		text = cursor.Error()
	case errors.As(err, &field):
		text = field.Error()
	case errors.As(err, &operator):
		text = operator.Error()
	case errors.As(err, &value):
		text = value.Error()
	case errors.As(err, &name):
		text = name.Error()
	case errors.As(err, &id):
		text = id.Error()
	default:
		for _, sentinel := range []error{blobfs.ErrInvalidName, blobfs.ErrInvalidPath, blobfs.ErrInvalidKey,
			blobfs.ErrInvalidID, blobfs.ErrRootDirectory, query.ErrDirectives} {
			if errors.Is(err, sentinel) {
				text = sentinel.Error()
				break
			}
		}
	}
	text = strings.TrimPrefix(text, "blobfs: ")
	return strings.TrimPrefix(text, "query: ")
}

// Conflict is the 409 problem carrying detail, one of the Detail
// constants; a domain's matcher builds its own conflicts with it.
func Conflict(detail string) web.Problem {
	return web.Problem{Status: http.StatusConflict, Detail: detail}
}
