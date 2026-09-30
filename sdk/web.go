package sdk

import (
	"net/http"

	"github.com/standards-lab/go-web-sdk"
)

// Command reads a guarded command's three inputs in the order the domain
// architecture fixes: the {id} path value, the If-Match version, and the
// body as one strict JSON value bounded at limit bytes. The first
// rejection is the error, each the SDK's own; a handler returns it
// unchanged. A guarded command without a body reads [web.PathUUID] and
// [web.IfMatch] itself.
func Command[T any](w http.ResponseWriter, r *http.Request, limit int64) (id string, version int64, body T, err error) {
	if id, err = web.PathUUID(r, "id"); err != nil {
		return "", 0, body, err
	}
	if version, err = web.IfMatch(r); err != nil {
		return "", 0, body, err
	}
	if body, err = web.DecodeJSON[T](w, r, limit); err != nil {
		return "", 0, body, err
	}
	return id, version, body, nil
}
