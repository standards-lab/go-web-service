package document

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// maxCommandBody bounds a command's request body; a command carries a few
// fields, never bulk data.
const maxCommandBody = 1 << 16

// maxFileBody bounds an upload at 10 MiB, the layer's limit for one
// document.
const maxFileBody = 10 << 20

// handler binds the layer's endpoints to its service under the injected
// paging policy. Every handler returns its error; the group's writer maps
// it to a problem.
type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, rooted at /documents, every route
// under /{org}, the organization's id, where a directory's {id} may be the
// root alias. The directories: create (POST /directories), the metadata read
// with the path (GET /directories/{id}), the paged listings of its child
// directories and its files, the delete (DELETE /directories/{id}, with
// ?recursive=true to empty it first), and the move, an action on its own
// path (POST /directories/{id}/move). An upload is a PUT of the raw body to
// the new file's name in its directory (PUT /directories/{id}/files/{name}).
// The files: the metadata read (GET /files/{id}), the download
// (GET /files/{id}/content), the delete, and the move. The moves take their
// version precondition from If-Match. Every rejection is an RFC 9457
// problem through the group's error writer: the SDK maps its own request
// errors, the layer's matcher its own vocabulary, and the data package's
// matcher the library's. The composition root mounts the group into the API
// module and supplies limits from the service's reads configuration.
func Routes(service *Service, limits web.Limits) *web.Group {
	h := &handler{service: service, limits: limits}
	g := web.NewGroup("/documents")
	g.SetErrorWriter(web.NewErrorWriter(status, data.Status))
	g.HandleErr("POST", "/{org}/directories", h.createDirectory)
	g.HandleErr("GET", "/{org}/directories/{id}", h.directory)
	g.HandleErr("GET", "/{org}/directories/{id}/directories", h.listDirectories)
	g.HandleErr("GET", "/{org}/directories/{id}/files", h.listFiles)
	g.HandleErr("DELETE", "/{org}/directories/{id}", h.deleteDirectory)
	g.HandleErr("POST", "/{org}/directories/{id}/move", h.moveDirectory)
	g.HandleErr("PUT", "/{org}/directories/{id}/files/{name}", h.putFile)
	g.HandleErr("GET", "/{org}/files/{id}", h.file)
	g.HandleErr("GET", "/{org}/files/{id}/content", h.content)
	g.HandleErr("DELETE", "/{org}/files/{id}", h.deleteFile)
	g.HandleErr("POST", "/{org}/files/{id}/move", h.moveFile)
	return g
}

func (h *handler) createDirectory(w http.ResponseWriter, r *http.Request) error {
	org, err := sdk.PathID(r, "org")
	if err != nil {
		return err
	}
	body, err := web.DecodeJSON[CreateDirectory](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.CreateDirectory(r.Context(), org, body)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path+"/"+ident.ID)
	return web.WriteJSON(w, http.StatusCreated, ident)
}

func (h *handler) directory(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	d, err := h.service.Directory(r.Context(), org, id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, d)
}

func (h *handler) listDirectories(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, paging, err := h.service.ListDirectories(r.Context(), org, id, q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, paging))
}

func (h *handler) listFiles(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, paging, err := h.service.ListFiles(r.Context(), org, id, q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, paging))
}

// deleteDirectory reads ?recursive as a boolean; absent is false.
func (h *handler) deleteDirectory(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	recursive := false
	if raw := r.URL.Query().Get("recursive"); raw != "" {
		if recursive, err = strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("%w: recursive must be true or false, not %q", ErrValidation, raw)
		}
	}
	if err := h.service.DeleteDirectory(r.Context(), org, id, recursive); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// moveDirectory reads its inputs in the guarded command's order, the path
// ids, the If-Match version, then the body; sdk.Command's path read does
// not take the root alias, which the service refuses with its own reason.
func (h *handler) moveDirectory(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	version, err := web.IfMatch(r)
	if err != nil {
		return err
	}
	body, err := web.DecodeJSON[MoveDirectory](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.MoveDirectory(r.Context(), org, id, version, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ident)
}

// putFile accepts any media type: the download never renders the bytes,
// so the type is only what the client declared. The Location is the new
// file's metadata.
func (h *handler) putFile(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	upload, err := web.ReadUpload(w, r, maxFileBody)
	if err != nil {
		return err
	}
	ident, err := h.service.PutFile(r.Context(), org, id, r.PathValue("name"), upload)
	if err != nil {
		return err
	}
	prefix, _, _ := strings.Cut(r.URL.Path, "/directories/")
	w.Header().Set("Location", prefix+"/files/"+ident.ID)
	return web.WriteJSON(w, http.StatusCreated, ident)
}

func (h *handler) file(w http.ResponseWriter, r *http.Request) error {
	org, id, err := filePath(r)
	if err != nil {
		return err
	}
	f, err := h.service.File(r.Context(), org, id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, f)
}

// content proxies an available file's bytes as an attachment: user
// content is never rendered inline, since stored HTML would run in the
// API's origin. The organization's documents are for no shared cache, and
// a client revalidates by ETag on every use.
func (h *handler) content(w http.ResponseWriter, r *http.Request) error {
	org, id, err := filePath(r)
	if err != nil {
		return err
	}
	c, err := h.service.Content(r.Context(), org, id)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", attachment(c.Name))
	w.Header().Set("Cache-Control", "private, no-cache")
	return web.WriteObject(w, r, c.Object, c.Open)
}

func (h *handler) deleteFile(w http.ResponseWriter, r *http.Request) error {
	org, id, err := filePath(r)
	if err != nil {
		return err
	}
	if err := h.service.DeleteFile(r.Context(), org, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) moveFile(w http.ResponseWriter, r *http.Request) error {
	org, err := sdk.PathID(r, "org")
	if err != nil {
		return err
	}
	id, version, body, err := sdk.Command[MoveFile](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.MoveFile(r.Context(), org, id, version, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ident)
}

// directoryPath reads the organization's id and a directory's, the root
// alias or a UUID in canonical form.
func directoryPath(r *http.Request) (org, id string, err error) {
	if org, err = sdk.PathID(r, "org"); err != nil {
		return "", "", err
	}
	if r.PathValue("id") == RootAlias {
		return org, RootAlias, nil
	}
	id, err = sdk.PathID(r, "id")
	return org, id, err
}

// filePath reads the organization's id and a file's.
func filePath(r *http.Request) (org, id string, err error) {
	if org, err = sdk.PathID(r, "org"); err != nil {
		return "", "", err
	}
	id, err = sdk.PathID(r, "id")
	return org, id, err
}

// attachment is the Content-Disposition of a download named name (RFC
// 6266): the quoted filename, a quote or a backslash escaped as a quoted
// pair, and for a name outside printable ASCII a fallback with each such
// rune replaced by an underscore, followed by the exact name as filename*
// in RFC 8187's encoding, which a recipient prefers.
func attachment(name string) string {
	var fallback strings.Builder
	ascii := true
	for _, c := range name {
		switch {
		case c == '"' || c == '\\':
			fallback.WriteByte('\\')
			fallback.WriteRune(c)
		case c < 0x20 || c > 0x7e:
			ascii = false
			fallback.WriteByte('_')
		default:
			fallback.WriteRune(c)
		}
	}
	header := `attachment; filename="` + fallback.String() + `"`
	if ascii {
		return header
	}
	var encoded strings.Builder
	for i := 0; i < len(name); i++ {
		if b := name[i]; attrChar(b) {
			encoded.WriteByte(b)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", b)
		}
	}
	return header + "; filename*=UTF-8''" + encoded.String()
}

// attrChar reports whether b is an RFC 8187 attr-char, the bytes an
// extended value carries unencoded.
func attrChar(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", b) >= 0
}

// status is the layer's own error vocabulary as one web.ProblemMatcher: a
// validation rejection or a malformed path id (400). The SDK's request
// errors map themselves, and the library's vocabulary (blobfs's and the
// object store's sentinels, directives, the missing row, constraint
// violations, the stale version, the outage) is data.Status, composed
// after this one.
func status(err error) (web.Problem, bool) {
	var path *sdk.PathError
	if errors.Is(err, ErrValidation) || errors.As(err, &path) {
		return web.Problem{Status: http.StatusBadRequest}, true
	}
	return web.Problem{}, false
}
