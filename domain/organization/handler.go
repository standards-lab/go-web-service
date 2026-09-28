package organization

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// maxCommandBody bounds a command's request body; a command carries a few
// fields, never bulk data.
const maxCommandBody = 1 << 16

// maxLogoBody bounds a logo upload at 1 MiB: an organization's mark, never
// a photograph.
const maxLogoBody = 1 << 20

// handler binds the layer's endpoints to its service under the injected
// paging policy. Every handler returns its error; the group's writer maps
// it to a problem.
type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, rooted at /organizations. The
// reads: the paginated list, the id read, and the path read, a lookup that
// takes the path as its query (/lookup?path=/acme/engineering) so no
// wildcard route overlaps a sub-resource of /{id}. The commands: create
// (POST), edit (PUT /{id}), transfer (POST /{id}/transfer), and delete
// (DELETE /{id}); the guarded three take their version precondition from
// If-Match. The logo is a sub-resource at /{id}/logo: PUT stores the raw
// body as the active logo, replacing any; GET proxies its bytes,
// revalidated by ETag; and DELETE retires it. Every rejection is an RFC
// 9457 problem through the group's error writer: the SDK maps its own request errors, the layer's matcher
// its own vocabulary, and the data package's matcher the library's. The
// composition root mounts the group into the API module and supplies
// limits from the service's reads configuration.
func Routes(service *Service, limits web.Limits) *web.Group {
	h := &handler{service: service, limits: limits}
	g := web.NewGroup("/organizations")
	g.SetErrorWriter(web.NewErrorWriter(status, data.Status))
	g.HandleErr("GET", "", h.list)
	g.HandleErr("GET", "/{id}", h.find)
	g.HandleErr("GET", "/lookup", h.lookup)
	g.HandleErr("POST", "", h.create)
	g.HandleErr("PUT", "/{id}", h.edit)
	g.HandleErr("POST", "/{id}/transfer", h.transfer)
	g.HandleErr("DELETE", "/{id}", h.delete)
	g.HandleErr("PUT", "/{id}/logo", h.putLogo)
	g.HandleErr("GET", "/{id}/logo", h.logo)
	g.HandleErr("DELETE", "/{id}/logo", h.deleteLogo)
	return g
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) error {
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, paging, err := h.service.List(r.Context(), q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, paging))
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) error {
	id, err := sdk.PathID(r, "id")
	if err != nil {
		return err
	}
	o, err := h.service.Find(r.Context(), id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, o)
}

// lookup reads the organization at the composed path the query's path
// names, with or without its leading slash; an absent path is a validation
// rejection.
func (h *handler) lookup(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Query().Get("path")
	if path == "" {
		return fmt.Errorf("%w: lookup requires a path query parameter, like ?path=/acme/engineering", ErrValidation)
	}
	o, err := h.service.FindByPath(r.Context(), "/"+strings.TrimPrefix(path, "/"))
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, o)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[CreateOrganization](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.Create(r.Context(), body)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path+"/"+ident.ID)
	return web.WriteJSON(w, http.StatusCreated, ident)
}

func (h *handler) edit(w http.ResponseWriter, r *http.Request) error {
	id, version, body, err := sdk.Command[EditOrganization](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.Edit(r.Context(), id, version, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ident)
}

func (h *handler) transfer(w http.ResponseWriter, r *http.Request) error {
	id, version, body, err := sdk.Command[TransferOrganization](w, r, maxCommandBody)
	if err != nil {
		return err
	}
	ident, err := h.service.Transfer(r.Context(), id, version, body)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, ident)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) error {
	id, err := sdk.PathID(r, "id")
	if err != nil {
		return err
	}
	version, err := web.IfMatch(r)
	if err != nil {
		return err
	}
	if err := h.service.Delete(r.Context(), id, version); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) putLogo(w http.ResponseWriter, r *http.Request) error {
	id, err := sdk.PathID(r, "id")
	if err != nil {
		return err
	}
	upload, err := web.ReadUpload(w, r, maxLogoBody)
	if err != nil {
		return err
	}
	ident, err := h.service.PutLogo(r.Context(), id, upload)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path)
	return web.WriteJSON(w, http.StatusCreated, ident)
}

// logo proxies the active logo's bytes. no-cache has a client revalidate
// by ETag on every use, since a replacement changes the logo under the same
// URL.
func (h *handler) logo(w http.ResponseWriter, r *http.Request) error {
	id, err := sdk.PathID(r, "id")
	if err != nil {
		return err
	}
	logo, err := h.service.Logo(r.Context(), id)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-cache")
	return web.WriteObject(w, r, logo.Object, logo.Open)
}

func (h *handler) deleteLogo(w http.ResponseWriter, r *http.Request) error {
	id, err := sdk.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := h.service.DeleteLogo(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// status is the layer's own error vocabulary as one web.ProblemMatcher: a
// validation rejection or a malformed path id (400) and the cycle (409),
// which carries data.DetailConflict as every conflict data.Status reports
// without a text of its own does. The SDK's request errors map themselves,
// and the library's vocabulary (directives, the missing row, constraint
// violations, the stale version, the outage) is data.Status, composed after
// this one.
func status(err error) (web.Problem, bool) {
	var path *sdk.PathError
	switch {
	case errors.Is(err, ErrValidation), errors.As(err, &path):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, ErrCycle):
		return data.Conflict(data.DetailConflict), true
	}
	return web.Problem{}, false
}
