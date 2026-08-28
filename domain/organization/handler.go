package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"uuid"

	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/operation"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-service/sdk"
)

// maxCommandBody bounds a command's request body; a command carries a few
// fields, never bulk data.
const maxCommandBody = 1 << 16

// handler binds the layer's endpoints to its service under the injected
// paging policy, every rejection routed through the layer's error writer.
type handler struct {
	service *Service
	limits  web.Limits
	errors  *web.ErrorWriter
}

// Routes builds the layer's route group, rooted at /organizations. The
// reads: the paginated list, the id read, and the path read. The commands:
// create (POST), edit (PATCH /{id}), transfer (POST /{id}/transfer — the
// action route, since why the record changes determines its validation),
// and delete (DELETE /{id}); the guarded three take their version
// precondition from If-Match. Every rejection is an RFC 9457 problem
// through the layer's error writer, whose matcher list is the layer's own
// error vocabulary — wired here, not composition-root policy. The
// composition root mounts the group into the API module and supplies limits
// from the service's reads configuration.
func Routes(service *Service, limits web.Limits) *web.Group {
	h := &handler{
		service: service,
		limits:  limits,
		errors:  web.NewErrorWriter(status),
	}

	g := web.NewGroup("/organizations")
	g.HandleFunc("GET", "", h.list)
	g.HandleFunc("GET", "/{id}", h.find)
	g.HandleFunc("GET", "/path/{path...}", h.findByPath)
	g.HandleFunc("POST", "", h.create)
	g.HandleFunc("PATCH", "/{id}", h.edit)
	g.HandleFunc("POST", "/{id}/transfer", h.transfer)
	g.HandleFunc("DELETE", "/{id}", h.delete)

	return g
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	q, err := web.ParseQuery(r.URL.Query(), h.limits)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	items, total, err := h.service.List(r.Context(), q)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, web.NewPage(items, q, total))
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	o, err := h.service.Find(r.Context(), id)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, o)
}

func (h *handler) findByPath(w http.ResponseWriter, r *http.Request) {
	o, err := h.service.FindByPath(r.Context(), "/"+r.PathValue("path"))
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, o)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	body, err := decode[CreateOrganization](w, r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	ident, err := h.service.Create(r.Context(), body)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	w.Header().Set("Location", r.URL.Path+"/"+ident.ID)
	_ = web.WriteJSON(w, http.StatusCreated, ident)
}

func (h *handler) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	version, err := sdk.IfMatch(r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	body, err := decode[EditOrganization](w, r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	ident, err := h.service.Edit(r.Context(), id, version, body)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, ident)
}

func (h *handler) transfer(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	version, err := sdk.IfMatch(r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	body, err := decode[TransferOrganization](w, r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	ident, err := h.service.Transfer(r.Context(), id, version, body)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	_ = web.WriteJSON(w, http.StatusOK, ident)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	version, err := sdk.IfMatch(r)
	if err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	if err := h.service.Delete(r.Context(), id, version); err != nil {
		_ = h.errors.Write(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathID reads and validates the {id} path value, writing the rejection
// itself so each handler keeps to its own flow.
func (h *handler) pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = h.errors.Write(w, r, fmt.Errorf("%w: id must be a UUID", ErrValidation))
		return "", false
	}
	return id, true
}

// decode reads a command body strictly: unknown fields are rejected so a
// misspelled field cannot silently change a command's meaning, and any
// decode failure is a validation rejection carrying the reason as its
// detail.
func decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCommandBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("%w: body: %v", ErrValidation, err)
	}
	return v, nil
}

// status is the layer's error vocabulary as one [web.StatusMatcher]: the
// precondition pair (428 missing, 400 malformed), the validation and
// read-contract rejections (400), the missing row (404), the state
// conflicts — cycle, unique, foreign key — (409), and the failed version
// precondition (412). The database's check and not-null violations stay
// unmatched on purpose: service validation owns those rules, so a breach is
// an invariant failure (500), not a client error.
func status(err error) (int, bool) {
	var precondition *sdk.PreconditionError
	var field *operation.UnknownFieldError
	var operator *operation.UnknownOperatorError
	switch {
	case errors.As(err, &precondition):
		if precondition.Missing {
			return http.StatusPreconditionRequired, true
		}
		return http.StatusBadRequest, true
	case errors.Is(err, ErrValidation),
		errors.As(err, &field),
		errors.As(err, &operator):
		return http.StatusBadRequest, true
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound, true
	case errors.Is(err, ErrCycle),
		errors.Is(err, database.ErrUniqueViolation),
		errors.Is(err, database.ErrForeignKeyViolation):
		return http.StatusConflict, true
	case errors.Is(err, database.ErrVersionMismatch):
		return http.StatusPreconditionFailed, true
	}
	return 0, false
}
