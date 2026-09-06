package organization

import (
	"errors"
	"net/http"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

// maxCommandBody bounds a command's request body; a command carries a few
// fields, never bulk data.
const maxCommandBody = 1 << 16

// handler binds the layer's endpoints to its service under the injected
// paging policy. Every handler returns its error; the group's writer maps
// it to a problem.
type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, rooted at /organizations. The
// reads: the paginated list, the id read, and the path read. The commands:
// create (POST), edit (PUT /{id}), transfer (POST /{id}/transfer), and
// delete (DELETE /{id}); the guarded three take their version precondition
// from If-Match. Every rejection is an RFC 9457 problem through the group's
// error writer: the SDK maps its own request errors, the layer's matcher
// its own vocabulary, and the data package's matcher the library's. The
// composition root mounts the group into the API module and supplies
// limits from the service's reads configuration.
func Routes(service *Service, limits web.Limits) *web.Group {
	h := &handler{service: service, limits: limits}
	g := web.NewGroup("/organizations")
	g.SetErrorWriter(web.NewErrorWriter(status, data.Status))
	g.HandleErr("GET", "", h.list)
	g.HandleErr("GET", "/{id}", h.find)
	g.HandleErr("GET", "/path/{path...}", h.findByPath)
	g.HandleErr("POST", "", h.create)
	g.HandleErr("PUT", "/{id}", h.edit)
	g.HandleErr("POST", "/{id}/transfer", h.transfer)
	g.HandleErr("DELETE", "/{id}", h.delete)
	return g
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) error {
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		return err
	}
	items, total, err := h.service.List(r.Context(), q)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, total))
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

func (h *handler) findByPath(w http.ResponseWriter, r *http.Request) error {
	o, err := h.service.FindByPath(r.Context(), "/"+r.PathValue("path"))
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

// status is the layer's own error vocabulary as one web.StatusMatcher: a
// validation rejection or a malformed path id (400) and the cycle (409).
// The SDK's request errors map themselves, and the library's vocabulary
// (directives, the missing row, constraint violations, the stale version,
// the outage) is data.Status, composed after this one.
func status(err error) (int, bool) {
	var path *sdk.PathError
	switch {
	case errors.Is(err, ErrValidation), errors.As(err, &path):
		return http.StatusBadRequest, true
	case errors.Is(err, ErrCycle):
		return http.StatusConflict, true
	}
	return 0, false
}
