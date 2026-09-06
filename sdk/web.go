package sdk

import (
	"fmt"
	"net/http"
	"uuid"
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
