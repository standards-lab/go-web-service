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
	// DetailNameTaken is a taken name or id.
	DetailNameTaken = "an entry with that name already exists"
	// DetailNotEmpty is a delete of a directory with contents.
	DetailNotEmpty = "the directory is not empty"
	// DetailDeleting is a change a deleting directory refused: the row's
	// own branch is marked, or the change reaches into one.
	DetailDeleting = "the directory is being deleted"
	// DetailFileDeleting is a change refused by the file's own delete (a
	// blobfs.DeletingError whose Directory is false).
	DetailFileDeleting = "the file is being deleted"
	// DetailReferenced is a delete of a file a row still references.
	DetailReferenced = "the file is referenced"
	// DetailConflict is every other conflict's: a constraint violation, a
	// move into its own subtree, a transition the file's status does not
	// allow, and a domain's own conflicts.
	DetailConflict = "the request conflicts with the current state"
)

// Status is the web.ProblemMatcher over the errors every domain's store
// returns. It matches blobfs's and go-storage's errors before the
// database's, since a blobfs violation also unwraps to the constraint
// error beneath it, and maps them to:
//
//   - 400: a rejected directive, a malformed name, path, key, or id, an
//     operation on the root, or ErrBodyRead; the detail is the refusal
//     alone, never the operation chain that wrapped it
//   - 404: an absent row, entry, or object
//   - 409: a taken name or id, a non-empty directory, a referenced file, a
//     deleting file or directory, a cycle, a transition the status does not
//     allow, or a unique or foreign-key violation, each with its Detail
//     constant and never the error's own text
//   - 412: a stale version
//   - 413: an object over the store's size bound
//   - 503: a database or store not ready or unreachable, a missing
//     container, and a pooled connection lost mid-read (an unexpected EOF
//     sqlate leaves unclassified)
//
// A handler composes it after its own matcher. Check and not-null
// violations stay unmatched: a command's validation owns those rules, so a
// breach is a server fault.
func Status(err error) (web.Problem, bool) {
	var deleting *blobfs.DeletingError
	switch {
	case errors.Is(err, ErrBodyRead):
		return web.Problem{Status: http.StatusBadRequest, Detail: ErrBodyRead.Error()}, true
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
