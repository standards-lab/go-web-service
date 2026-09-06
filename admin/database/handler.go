package database

import (
	"errors"
	"net/http"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"
)

// maxBody bounds an administrative request body: a step count or a
// version, never more.
const maxBody = 1 << 10

type handler struct {
	service *admin.Service
}

// Routes builds the admin domain's route group, rooted at /database. Reads:
// diagnostics, the schema status, the pattern catalog, and the statements
// registry. Operations: verify, up, down, steps, and force, each a POST
// whose response is the resulting status, and seed, whose response is the
// rows it inserted by table. force sets the history without running any
// file, the operator's override for dirty state after the schema has been
// repaired by hand. Every rejection is an RFC 9457 problem; a schema-state
// conflict and a disabled seed carry their reason as the detail, since an
// operator needs to know which version is dirty or pending, or that this
// environment does not seed. The composition root mounts the group into
// the admin mount. The confirmation token the strategy requires for down
// and force arrives with the management listener.
func Routes(service *admin.Service) *web.Group {
	h := &handler{service: service}
	ew := web.NewErrorWriter(status)
	ew.Detail(http.StatusForbidden, http.StatusConflict)
	g := web.NewGroup("/database")
	g.SetErrorWriter(ew)
	g.HandleErr("GET", "/diagnostics", h.diagnostics)
	g.HandleErr("GET", "/schema", h.status)
	g.HandleErr("GET", "/patterns", h.patterns)
	g.HandleErr("GET", "/statements", h.statements)
	g.HandleErr("POST", "/schema/verify", h.verify)
	g.HandleErr("POST", "/schema/up", h.up)
	g.HandleErr("POST", "/schema/down", h.down)
	g.HandleErr("POST", "/schema/steps", h.steps)
	g.HandleErr("POST", "/schema/force", h.force)
	g.HandleErr("POST", "/seed", h.seed)
	return g
}

func (h *handler) diagnostics(w http.ResponseWriter, r *http.Request) error {
	d, err := h.service.Diagnose(r.Context())
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, d)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(h.service.Status(r.Context()))
}

func (h *handler) patterns(w http.ResponseWriter, _ *http.Request) error {
	return web.WriteJSON(w, http.StatusOK, h.service.Catalog())
}

func (h *handler) statements(w http.ResponseWriter, _ *http.Request) error {
	return web.WriteJSON(w, http.StatusOK, h.service.Statements())
}

func (h *handler) verify(w http.ResponseWriter, r *http.Request) error {
	if err := h.service.Verify(r.Context()); err != nil {
		return err
	}
	return h.status(w, r)
}

func (h *handler) up(w http.ResponseWriter, r *http.Request) error {
	return respond(w)(h.service.Up(r.Context()))
}

// down reverts one migration when the request carries no body.
func (h *handler) down(w http.ResponseWriter, r *http.Request) error {
	body := Steps{Steps: 1}
	if r.ContentLength != 0 {
		var err error
		if body, err = web.DecodeJSON[Steps](w, r, maxBody); err != nil {
			return err
		}
	}
	return respond(w)(h.service.Down(r.Context(), body.Steps))
}

func (h *handler) steps(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Steps](w, r, maxBody)
	if err != nil {
		return err
	}
	return respond(w)(h.service.Steps(r.Context(), body.Steps))
}

func (h *handler) force(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Force](w, r, maxBody)
	if err != nil {
		return err
	}
	return respond(w)(h.service.Force(r.Context(), body.Version))
}

func (h *handler) seed(w http.ResponseWriter, r *http.Request) error {
	n, err := h.service.Seed(r.Context())
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, n)
}

// respond writes an operation's resulting status, or returns its error.
func respond(w http.ResponseWriter) func(admin.Status, error) error {
	return func(st admin.Status, err error) error {
		if err != nil {
			return err
		}
		return web.WriteJSON(w, http.StatusOK, st)
	}
}

// status is the domain's error vocabulary as one web.StatusMatcher: a
// rejected verb argument (400), a version outside the set (400), a seed
// the environment forbids (403), and the schema states an operation cannot
// proceed from, dirty, pending, a history the set does not carry, a
// migration with no down, as conflicts (409). A dialect without the lock
// capability stays unmatched: it is a wiring defect (500).
func status(err error) (int, bool) {
	switch {
	case errors.Is(err, admin.ErrValidation), errors.Is(err, migrate.ErrVersionNotFound):
		return http.StatusBadRequest, true
	case errors.Is(err, admin.ErrSeedDisabled):
		return http.StatusForbidden, true
	case errors.Is(err, migrate.ErrDirty),
		errors.Is(err, migrate.ErrPending),
		errors.Is(err, migrate.ErrUnknownVersion),
		errors.Is(err, migrate.ErrNoDown):
		return http.StatusConflict, true
	}
	return 0, false
}
