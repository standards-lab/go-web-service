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
