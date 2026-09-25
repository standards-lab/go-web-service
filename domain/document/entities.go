package document

import (
	"errors"
	"fmt"
	"io"
	"time"
	"uuid"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-web-sdk"
)

// ErrValidation classifies an input rejection; wrapped with the
// field-level reason, it reaches the wire as a 400's detail.
var ErrValidation = errors.New("invalid command")

// errRootMove refuses a move of the document root, which binds the
// organization's hierarchy where it stands.
var errRootMove = fmt.Errorf("%w: the document root cannot be moved", ErrValidation)

// RootAlias is the directory id segment, or body value, that names the
// organization's document root without its id.
const RootAlias = "root"

// Directory is one directory of an organization's hierarchy, as the API
// presents it. The document root has no parent and is named "/". Path is
// the directory's path from the root, "/" for the root itself; only the
// single-directory read computes it, so a listing omits it. Version is the
// concurrency token a move guards on.
type Directory struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// File is one file of an organization's hierarchy, as the API presents
// it: the row's description without its object key. Status is the stage
// of blobfs's protocols the file stands at, and Size is nil until its
// write completes.
type File struct {
	ID          string        `json:"id"`
	DirectoryID string        `json:"directory_id"`
	Name        string        `json:"name"`
	Status      blobfs.Status `json:"status"`
	Size        *int64        `json:"size"`
	ContentType string        `json:"content_type"`
	Version     int64         `json:"version"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Content is an available file as the download serves it: its name, for
// the attachment's filename; the stored object's description, which
// answers a revalidation alone; and Open, which streams the bytes and runs
// only when they are sent.
type Content struct {
	Name   string
	Object web.Object
	Open   func() (io.ReadCloser, error)
}

// CreateDirectory is the create command's input: the parent, the root
// alias or a directory id, and the new directory's name.
type CreateDirectory struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

// Validate rejects a missing or malformed parent and a name blobfs would
// refuse; the parent's scope and the name's uniqueness are the store's.
func (c CreateDirectory) Validate() error {
	return errors.Join(validDirectory("parent_id", c.ParentID), validName(c.Name))
}

// MoveDirectory is the directory move's input: the new parent, the root
// alias or a directory id, and the name, the current one for a move alone.
// Both keys are required: a move is structural, so neither is defaulted.
type MoveDirectory struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

// Validate rejects a missing or malformed parent and a name blobfs would
// refuse.
func (m MoveDirectory) Validate() error {
	return errors.Join(validDirectory("parent_id", m.ParentID), validName(m.Name))
}

// MoveFile is the file move's input: the destination directory, the root
// alias or a directory id, and the name, the current one for a move alone.
type MoveFile struct {
	DirectoryID string `json:"directory_id"`
	Name        string `json:"name"`
}

// Validate rejects a missing or malformed directory and a name blobfs
// would refuse.
func (m MoveFile) Validate() error {
	return errors.Join(validDirectory("directory_id", m.DirectoryID), validName(m.Name))
}

// Identity is every command's success envelope: the row's id and the
// version the command left it at. Commands return nothing else.
type Identity struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// The field rules the commands and the upload share.

// validDirectory accepts the root alias or a UUID in the named field.
func validDirectory(field, id string) error {
	if id == "" {
		return fmt.Errorf("%w: %s is required: root or a directory id", ErrValidation, field)
	}
	if id == RootAlias {
		return nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: %s must be root or a UUID", ErrValidation, field)
	}
	return nil
}

// validName mirrors blobfs's own name rule over the normalized name, so a
// name it would refuse is a 400 before any I/O.
func validName(name string) error {
	if err := blobfs.ValidateName(blobfs.NormalizeName(name)); err != nil {
		return fmt.Errorf("%w: name: %w", ErrValidation, err)
	}
	return nil
}
