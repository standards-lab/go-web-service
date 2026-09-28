package document

import (
	"github.com/standards-lab/go-web-sdk"
)

// RootAlias is the directory id segment, or body value, that names the
// organization's document root without its id: domain/document's
// RootAlias.
const RootAlias = "root"

// DefaultPageSize is the page size a listing gets when it names none:
// internal/config/reads.go's defaultReadsDefaultSize, which config.json
// leaves in force for the document listings as for the organization list.
const DefaultPageSize = 20

// CreateDirectory is the dirs create command's body, the shape of
// domain/document's CreateDirectory: the parent, the root alias or a
// directory id, and the new directory's name.
type CreateDirectory struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

// MoveDirectory is the dirs move command's body, the shape of
// domain/document's MoveDirectory: the new parent and the name, the current
// one for a move alone. The service requires both keys.
type MoveDirectory struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

// MoveFile is the files move command's body, the shape of domain/document's
// MoveFile: the destination directory and the name, the current one for a
// move alone.
type MoveFile struct {
	DirectoryID string `json:"directory_id"`
	Name        string `json:"name"`
}

// Identity is every command's success envelope, the shape of
// domain/document's Identity: the row's id and the version the command left
// it at.
type Identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// Directory is one directory as the API presents it. Path is set on the
// single-directory read only. The service's own type also carries status,
// created_at, and updated_at; slab reads none of them, so they are left
// unstated and pass through untouched in the raw JSON a command prints.
type Directory struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
	Path     string  `json:"path,omitempty"`
	Version  int64   `json:"version"`
}

// File is one file's metadata as the API presents it. Status is blobfs's
// stage name, a string here since slab does not import blobfs, and Size is
// nil until the write completes. The timestamps are left unstated, as on
// Directory.
type File struct {
	ID          string `json:"id"`
	DirectoryID string `json:"directory_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Size        *int64 `json:"size"`
	ContentType string `json:"content_type"`
	Version     int64  `json:"version"`
}

// DirectoryPage is the child-directory listing's envelope: go-web-sdk's own
// paginated success envelope, which the service writes as is.
type DirectoryPage = web.Page[Directory]

// FilePage is the file listing's envelope, the same SDK envelope around
// File.
type FilePage = web.Page[File]
