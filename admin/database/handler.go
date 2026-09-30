package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
)

// maxBody bounds an administrative request body: a set name and a step
// count or a version, never more.
const maxBody = 1 << 10

// ErrUnconfirmed rejects a reset whose body does not confirm it.
var ErrUnconfirmed = errors.New(`database: a reset reverts every migration set and its rows; it requires "confirm": true`)

// SchemaGate is what the domain asks of the process while a verb changes
// the schema: that its background work over the schema pause. Exclusive
// waits for the turn in flight to finish and holds every later turn off
// until the release, or returns ctx's error, holding nothing, if ctx ends
// first. The composition root injects the process's quiesce gate, which
// the sweep holds shared for each pass.
//
// A schema change and a sweep pass deadlock without it: reverting the
// migration that drops organization_directory, or the one that alters its
// cascading foreign key, locks that table and blobfs_directory in the
// opposite order from a pass's directory removal, so Postgres aborts one
// (SQLSTATE 40P01). The gate is per process: with several replicas,
// another replica's sweep can still meet a reset. A reset is a development
// operation; a multi-replica reset would need a database lock the sweep
// takes too.
type SchemaGate interface {
	Exclusive(ctx context.Context) (release func(), err error)
}

type handler struct {
	service *admin.Service
	gate    SchemaGate
}

// Routes builds the admin domain's route group, /database: the reads
// (diagnostics, the schema status, the pattern catalog, the statements
// registry, the named states) and the verbs (verify, up, down, steps,
// force, seed, and the state reset), which the README's admin section
// lists. The schema verbs answer with the resulting status. up, down,
// steps, and state change the schema, so each holds gate exclusively; the
// others take no lock the sweep contends for. A set name that is empty or
// undeclared is refused before any I/O. A schema-state conflict and a
// disabled seed carry their reason as the detail, since an operator needs
// to know which set is dirty or pending, or that no seed is configured.
// logger records the cause of every 5xx the group's error writer sends.
func Routes(service *admin.Service, gate SchemaGate, logger *slog.Logger) *web.Group {
	h := &handler{service: service, gate: gate}
	ew := web.NewErrorWriter(logger, status)
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
	return respond(w)(exclusive(r.Context(), h.gate, h.service.Up))
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
	return respond(w)(exclusive(r.Context(), h.gate, func(ctx context.Context) (admin.Status, error) {
		return h.service.Down(ctx, body.Set, body.Steps)
	}))
}

func (h *handler) steps(w http.ResponseWriter, r *http.Request) error {
	body, err := web.DecodeJSON[Steps](w, r, maxBody)
	if err != nil {
		return err
	}
	return respond(w)(exclusive(r.Context(), h.gate, func(ctx context.Context) (admin.Status, error) {
		return h.service.Steps(ctx, body.Set, body.Steps)
	}))
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
	tr, err := exclusive(r.Context(), h.gate, func(ctx context.Context) (admin.Transition, error) {
		return h.service.Reset(ctx, body.State)
	})
	if err != nil {
		return err
	}
	return web.WriteJSON(w, http.StatusOK, tr)
}

// exclusive runs op holding gate exclusively, and releases it once op
// returns. A gate that ctx ended before it was held runs nothing and
// returns ctx's error.
func exclusive[T any](ctx context.Context, gate SchemaGate, op func(context.Context) (T, error)) (T, error) {
	release, err := gate.Exclusive(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer release()
	return op(ctx)
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

// status is the domain's error vocabulary as one web.ProblemMatcher. A
// rejected verb argument (a version outside its set among them), a set the
// migrator does not declare, a state the seeder does not declare, and an
// unconfirmed reset are 400; a seed the environment cannot serve is 403;
// a schema state the operation cannot proceed from, admin.ErrConflict, is
// 409. A dialect without the lock capability stays unmatched: it is a
// wiring defect (500).
func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, admin.ErrValidation), errors.Is(err, admin.ErrUnknownSet),
		errors.Is(err, admin.ErrUnknownState), errors.Is(err, ErrUnconfirmed):
		return web.Problem{Status: http.StatusBadRequest}, true
	case errors.Is(err, admin.ErrSeedDisabled):
		return web.Problem{Status: http.StatusForbidden}, true
	case errors.Is(err, admin.ErrConflict):
		return web.Problem{Status: http.StatusConflict}, true
	}
	return web.Problem{}, false
}
