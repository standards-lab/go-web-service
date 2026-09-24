package database

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"
)

// maxBody bounds an administrative request body: a set name and a step
// count or a version, never more.
const maxBody = 1 << 10

// ErrUnconfirmed rejects a reset whose body does not confirm it.
var ErrUnconfirmed = errors.New(`database: a reset reverts every migration set and its rows; it requires "confirm": true`)

type handler struct {
	service *admin.Service
}

// Routes builds the admin domain's route group, rooted at /database. Reads:
// diagnostics, the schema status, the pattern catalog, the statements
// registry, and the named states.
//
// Operations, registered below:
//
//   - verify and up: each a POST over every migration set, whose response
//     is the resulting status
//   - down, steps, and force: each a POST naming the migration set it acts
//     on, whose response is the resulting status
//   - seed: applies the configured set or the one its body names, and
//     answers with the rows it inserted by table
//   - state: resets the database to the state its body names, once the
//     body confirms it, and answers with the transition
//
// The status reports each migration set, in declaration order. force sets
// a set's history without running any file, the operator's override for
// dirty state after the schema has been repaired by hand. Every rejection
// is an RFC 9457 problem, and a set name that is empty or undeclared is
// refused before any I/O. A schema-state conflict and a disabled seed carry
// their reason as the detail, since an operator needs to know which set
// and version is dirty or pending, or that this environment names no seed.
// The composition root mounts the group into the admin mount. The
// confirmation token the strategy requires for down and force arrives with
// the management listener.
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
	g.HandleErr("GET", "/states", h.states)
	g.HandleErr("POST", "/schema/verify", h.verify)
	g.HandleErr("POST", "/schema/up", h.up)
	g.HandleErr("POST", "/schema/down", h.down)
	g.HandleErr("POST", "/schema/steps", h.steps)
	g.HandleErr("POST", "/schema/force", h.force)
	g.HandleErr("POST", "/seed", h.seed)
	g.HandleErr("POST", "/state", h.state)
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

func (h *handler) states(w http.ResponseWriter, _ *http.Request) error {
	return web.WriteJSON(w, http.StatusOK, h.service.States())
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

// down reverts one migration of the named set when the body omits steps.
func (h *handler) down(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Steps](w, r, maxBody)
	if err != nil {
		return err
	}
	if body.Steps == 0 {
		body.Steps = 1
	}
	return respond(w)(h.service.Down(r.Context(), body.Set, body.Steps))
}

func (h *handler) steps(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Steps](w, r, maxBody)
	if err != nil {
		return err
	}
	return respond(w)(h.service.Steps(r.Context(), body.Set, body.Steps))
}

func (h *handler) force(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Force](w, r, maxBody)
	if err != nil {
		return err
	}
	return respond(w)(h.service.Force(r.Context(), body.Set, body.Version))
}

// seed applies the configured set when the request carries no body.
func (h *handler) seed(w http.ResponseWriter, r *http.Request) error {
	var body State
	if r.ContentLength != 0 {
		var err error
		if body, err = web.DecodeJSON[State](w, r, maxBody); err != nil {
			return err
		}
	}
	n, err := h.service.Seed(r.Context(), body.State)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, n)
}

func (h *handler) state(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Reset](w, r, maxBody)
	if err != nil {
		return err
	}
	if !body.Confirm {
		return fmt.Errorf("%w (state %q)", ErrUnconfirmed, body.State)
	}
	tr, err := h.service.Reset(r.Context(), body.State)
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, tr)
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

// status is the domain's error vocabulary as one web.ProblemMatcher: a
// rejected verb argument (400), a set the migrator does not declare (400),
// a version outside the set (400), a state the seeder does not declare
// (400), an unconfirmed reset (400), a seed the environment cannot serve
// (403), and the schema states an operation cannot proceed from, dirty,
// pending, a history the set does not carry, a migration with no down, a
// set above that has applied migrations, a set below with pending ones, as
// conflicts (409). A dialect without the lock capability stays unmatched:
// it is a wiring defect (500).
func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, admin.ErrValidation), errors.Is(err, admin.ErrUnknownSet),
		errors.Is(err, migrate.ErrVersionNotFound), errors.Is(err, admin.ErrUnknownState),
		errors.Is(err, ErrUnconfirmed):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, admin.ErrSeedDisabled):
		return web.Problem{Status: http.StatusForbidden}, true
	case errors.Is(err, migrate.ErrDirty),
		errors.Is(err, migrate.ErrPending),
		errors.Is(err, migrate.ErrUnknownVersion),
		errors.Is(err, migrate.ErrNoDown),
		errors.Is(err, migrate.ErrAboveApplied),
		errors.Is(err, migrate.ErrBelowPending):
		return web.Problem{Status: http.StatusConflict}, true
	}
	return web.Problem{}, false
}
