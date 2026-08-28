package organization

import "time"

// Organization is one node of the organization hierarchy, as the read
// contract presents it. ParentID is nil at a root. Path is composed at read
// time from the lineage and never stored. Version is the concurrency token
// the writes slice will guard on.
type Organization struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateOrganization is the create command's input: the parent under which
// the organization is created — nil for a root — and its code and name.
type CreateOrganization struct {
	ParentID *string `json:"parent_id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
}

// EditOrganization is the edit command's input: full replacement of the two
// client-mutable descriptive fields.
type EditOrganization struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TransferOrganization is the transfer command's input. A nil ParentID —
// stated as null or omitted — moves the organization to the root.
type TransferOrganization struct {
	ParentID *string `json:"parent_id"`
}

// Identity is every command's success envelope: the row's id and the
// version the command left it at. Commands return nothing else.
type Identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}
