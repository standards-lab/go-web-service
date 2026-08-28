package sdk

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// PreconditionError reports a version precondition the request failed to
// state or stated unreadably: Missing marks an absent If-Match header, and
// otherwise Value carries the rejected header text.
type PreconditionError struct {
	Missing bool
	Value   string
}

func (e *PreconditionError) Error() string {
	if e.Missing {
		return "the request requires an If-Match header"
	}
	return fmt.Sprintf("If-Match %q: must be one entity-tag containing an integer version, like \"3\"", e.Value)
}

// IfMatch reads the request's version precondition (RFC 9110 §13.1.1):
// exactly one strong entity-tag whose opaque value is a base-10 integer —
// If-Match: "3". A missing header, a weak tag, the * form, a list, or a
// non-integer tag is a *[PreconditionError]. The parse is syntax only; a
// version no row can hold answers as a failed precondition, not a malformed
// one.
func IfMatch(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, &PreconditionError{Missing: true}
	}
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, &PreconditionError{Value: raw}
	}
	version, err := strconv.ParseInt(raw[1:len(raw)-1], 10, 64)
	if err != nil {
		return 0, &PreconditionError{Value: raw}
	}
	return version, nil
}
