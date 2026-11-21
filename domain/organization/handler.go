package organization

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/data"
	"github.com/standards-lab/go-web-service/sdk"
)

const maxCommandBody = 1 << 16

const maxLogoBody = 1 << 20

type handler struct {
	service *Service
	limits  web.Limits
	logos   web.Transfer
}

// Routes builds the layer's route group, /organizations: the list, the
// read by id, the lookup by path (/lookup?path=/acme/engineering, a query
// so no wildcard overlaps a sub-resource of /{id}), the four commands, and
// the logo at /{id}/logo; docs/api.md lists them. limits is
// the list's paging policy; transfer sizes the deadlines of a logo's upload
// and download from the layer's limit, maxLogoBody; and logger records the
// cause of every 5xx the group's error writer sends.
func Routes(service *Service, limits web.Limits, transfer func(limit int64) web.Transfer, logger *slog.Logger) *web.Group {
	h := &handler{service: service, limits: limits, logos: transfer(maxLogoBody)}
	ew := web.NewErrorWriter(logger, status, data.Status)
	g := web.NewGroup("/organizations")
	g.SetErrorWriter(ew)
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
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	o, err := h.service.Find(r.Context(), id)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, o)
}

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
	id, err := web.PathUUID(r, "id")
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
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	upload, err := web.ReadUpload(w, r, h.logos.Limit())
	if err != nil {
		return err
	}
	if err := h.logos.WidenUpload(w, r); err != nil {
		return err
	}
	ident, err := h.service.PutLogo(r.Context(), id, upload)
	if err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.Path)
	return web.WriteJSON(w, http.StatusCreated, ident)
}

func (h *handler) logo(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	logo, err := h.service.Logo(r.Context(), id)
	if err != nil {
		return err
	}
	if err := h.logos.WidenDownload(w, logo.Object.Size); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-cache")
	return web.WriteObject(w, r, logo.Object, logo.Open)
}

func (h *handler) deleteLogo(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.service.DeleteLogo(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, ErrValidation):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, ErrCycle):
		return data.Conflict(data.DetailConflict), true
	}
	return web.Problem{}, false
}
