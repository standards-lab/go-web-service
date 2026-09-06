package sdk

import (
	"fmt"
	"net/http"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
)

// PathError reports a path value that is not the identifier its route
// declares: Name is the path wildcard and Value the rejected text. A
// handler's matcher maps it to a 400.
type PathError struct {
	Name  string
	Value string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("path %s=%q: must be a UUID", e.Name, e.Value)
}

// PathID reads the request's {name} path value as a UUID and returns it in
// canonical form, so a store binds one spelling whatever the request sent.
// A value that does not parse is a *[PathError]. The parse is syntax only;
// whether a row carries the id is the store's answer.
func PathID(r *http.Request, name string) (string, error) {
	raw := r.PathValue(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", &PathError{Name: name, Value: raw}
	}
	return id.String(), nil
}

// Command reads a guarded command's three inputs in the order the domain
// architecture fixes: the {id} path value, the If-Match version, and the
// body as one strict JSON value bounded at limit bytes. The first
// rejection is the error, each the SDK's own or a *[PathError]; a handler
// returns it unchanged. A guarded command without a body reads PathID and
// [web.IfMatch] itself.
func Command[T any](w http.ResponseWriter, r *http.Request, limit int64) (id string, version int64, body T, err error) {
	if id, err = PathID(r, "id"); err != nil {
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
