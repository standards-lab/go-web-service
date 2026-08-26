package organization

import (
	"net/http"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/sdk"
)

// handler binds the layer's endpoints to its service under the injected
// paging policy.
type handler struct {
	service *Service
	limits  web.Limits
}

// Routes builds the layer's route group, rooted at /organizations: the
// paginated list, the id read, and the path read, each mapped to its
// [Service] method, with every rejection written as an RFC 9457 problem
// through the sdk package. The composition root mounts the group into the
// API module and supplies limits from the service's reads configuration.
func Routes(service *Service, limits web.Limits) *web.Group {
	h := &handler{service: service, limits: limits}

	g := web.NewGroup("/organizations")
	g.HandleFunc("GET", "", h.list)
	g.HandleFunc("GET", "/{id}", h.find)
	g.HandleFunc("GET", "/path/{path...}", h.findByPath)

	return g
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	d, filters, err := sdk.ParseRead(r.URL.Query(), h.limits)
	if err != nil {
		sdk.WriteError(w, r, err)
		return
	}
	items, total, err := h.service.List(r.Context(), d, filters)
	if err != nil {
		sdk.WriteError(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, web.NewPage(items, d, total))
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = web.WriteProblem(w, r, http.StatusBadRequest, "", "id must be a UUID")
		return
	}
	o, err := h.service.Find(r.Context(), id)
	if err != nil {
		sdk.WriteError(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, o)
}

func (h *handler) findByPath(w http.ResponseWriter, r *http.Request) {
	o, err := h.service.FindByPath(r.Context(), "/"+r.PathValue("path"))
	if err != nil {
		sdk.WriteError(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, o)
}
