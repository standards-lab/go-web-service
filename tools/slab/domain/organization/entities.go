package organization

import (
	"github.com/standards-lab/go-web-sdk"
)

// DefaultPageSize is the page size a list request gets when it names none:
// internal/config/reads.go's defaultReadsDefaultSize, which config.json
// leaves in force.
const DefaultPageSize = 20

// CreateOrganization is the create command's body, the shape of
// domain/organization's CreateOrganization: the parent under which the
// organization is created, null for a root, and its code and name.
type CreateOrganization struct {
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
}

// EditOrganization is the edit command's body, the shape of
// domain/organization's EditOrganization: a full replacement of the two
// descriptive fields.
type EditOrganization struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TransferOrganization is the transfer command's body, the shape of
// domain/organization's TransferOrganization: the new parent, or null for
// the root. The service requires the parent_id key to be present and rejects
// a body without it, so a nil ParentID here always marshals as an explicit
// null; slab only ever marshals this type, so the service's own decoding
// machinery for distinguishing null from absent is not restated.
type TransferOrganization struct {
	ParentID *string `json:"parent_id"`
}

// Identity is every command's success envelope, the shape of
// domain/organization's Identity: the row's id and the version the command
// left it at.
type Identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// Organization is the read model as the API presents it. The service's own
// type also carries created_at and updated_at; slab reads neither, so they
// are left unstated and pass through untouched in the raw JSON a command
// prints.
type Organization struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Version  int64   `json:"version"`
	Path     string  `json:"path"`
}

// Page is the list's envelope around Organization: go-web-sdk's own paginated
// success envelope, which the service writes as is.
type Page = web.Page[Organization]
