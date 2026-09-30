package document

import (
	"errors"
	"fmt"
	"log/slog"
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
// paging policy and file transfer. Every handler returns its error; the
// group's writer maps it to a problem.
type handler struct {
	service *Service
	limits  web.Limits
	files   web.Transfer
}

// Routes builds the layer's route group, /documents/{org}, with the
// directory routes under /directories/{id} ({id} may be RootAlias) and the
// file routes under /files/{id}; the README's API section lists them. The
// moves and deletes take their version from If-Match. limits is the paging
// policy of the listings; transfer sizes the deadlines of a file's upload
// and download from the layer's limit, maxFileBody; and logger records the
// cause of every 5xx the group's error writer sends.
func Routes(service *Service, limits web.Limits, transfer func(limit int64) web.Transfer, logger *slog.Logger) *web.Group {
	h := &handler{service: service, limits: limits, files: transfer(maxFileBody)}
	ew := web.NewErrorWriter(logger, status, data.Status)
	g := web.NewGroup("/documents")
	g.SetErrorWriter(ew)
	g.HandleErr("POST", "/{org}/directories", h.createDirectory)
	g.HandleErr("GET", "/{org}/directories/{id}", h.directory)
	g.HandleErr("GET", "/{org}/directories/{id}/directories", h.listDirectories)
	g.HandleErr("GET", "/{org}/directories/{id}/files", h.listFiles)
	g.HandleErr("DELETE", "/{org}/directories/{id}", h.deleteDirectory)
	g.HandleErr("POST", "/{org}/directories/{id}/move", h.moveDirectory)
	g.HandleErr("POST", "/{org}/directories/{id}/files", h.uploadFile)
	g.HandleErr("GET", "/{org}/files/{id}", h.file)
	g.HandleErr("GET", "/{org}/files/{id}/content", h.content)
	g.HandleErr("DELETE", "/{org}/files/{id}", h.deleteFile)
	g.HandleErr("POST", "/{org}/files/{id}/move", h.moveFile)
	return g
}

func (h *handler) createDirectory(w http.ResponseWriter, r *http.Request) error {
	org, err := web.PathUUID(r, "org")
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

// deleteDirectory reads its inputs in the guarded command's order, the
// path ids, the If-Match version, then ?recursive as a boolean, absent
// false. An empty directory is removed, 204. A recursive delete marks the
// branch and answers 202 with no body, its Location the directory's read,
// which reports it deleting until the sweep removes it; a repeated one is
// accepted again.
func (h *handler) deleteDirectory(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	version, err := web.IfMatch(r)
	if err != nil {
		return err
	}
	recursive := false
	if raw := r.URL.Query().Get("recursive"); raw != "" {
		if recursive, err = strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("%w: recursive must be true or false, not %q", ErrValidation, raw)
		}
	}
	if !recursive {
		if err := h.service.DeleteDirectory(r.Context(), org, id, version); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	marked, err := h.service.DeleteBranch(r.Context(), org, id, version)
	if err != nil {
		return err
	}
	prefix, _, _ := strings.Cut(r.URL.Path, "/directories/")
	w.Header().Set("Location", prefix+"/directories/"+marked)
	w.WriteHeader(http.StatusAccepted)
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

// uploadFile reads the path ids, then the required name query parameter,
// then the body. It accepts any media type: the download never renders
// the bytes, so the type is only what the client declared. The Location is
// the new file's metadata.
func (h *handler) uploadFile(w http.ResponseWriter, r *http.Request) error {
	org, id, err := directoryPath(r)
	if err != nil {
		return err
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		return fmt.Errorf("%w: the name query parameter is required", ErrValidation)
	}
	upload, err := web.ReadUpload(w, r, h.files.Limit())
	if err != nil {
		return err
	}
	if err := h.files.WidenUpload(w, r); err != nil {
		return err
	}
	ident, err := h.service.UploadFile(r.Context(), org, id, name, upload)
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
	if err := h.files.WidenDownload(w, c.Object.Size); err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", web.Attachment(c.Name))
	w.Header().Set("Cache-Control", "private, no-cache")
	return web.WriteObject(w, r, c.Object, c.Open)
}

// deleteFile reads the path ids, then the If-Match version.
func (h *handler) deleteFile(w http.ResponseWriter, r *http.Request) error {
	org, id, err := filePath(r)
	if err != nil {
		return err
	}
	version, err := web.IfMatch(r)
	if err != nil {
		return err
	}
	if err := h.service.DeleteFile(r.Context(), org, id, version); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) moveFile(w http.ResponseWriter, r *http.Request) error {
	org, err := web.PathUUID(r, "org")
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
	if org, err = web.PathUUID(r, "org"); err != nil {
		return "", "", err
	}
	if r.PathValue("id") == RootAlias {
		return org, RootAlias, nil
	}
	id, err = web.PathUUID(r, "id")
	return org, id, err
}

// filePath reads the organization's id and a file's.
func filePath(r *http.Request) (org, id string, err error) {
	if org, err = web.PathUUID(r, "org"); err != nil {
		return "", "", err
	}
	id, err = web.PathUUID(r, "id")
	return org, id, err
}

// status is the layer's own error vocabulary as one web.ProblemMatcher: a
// validation rejection (400). The SDK maps its own request errors, a
// malformed path id among them, and the library's vocabulary is
// data.Status, composed after this one.
func status(err error) (web.Problem, bool) {
	if errors.Is(err, ErrValidation) {
		return web.Problem{Status: http.StatusBadRequest}, true
	}
	return web.Problem{}, false
}
