package database

import (
	"context"
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

// SchemaGate is what the domain asks of the process while a verb changes
// the schema: that the process's background work over the schema pause.
// Exclusive waits for that work's turn in flight to finish and holds every
// later turn off until the release, or returns ctx's error, holding
// nothing, if ctx ends first. The domain declares it and the composition
// root injects it; the process's quiesce gate satisfies it, and the sweep
// holds the same gate shared for each pass.
//
// The gate exists because a schema change and the sweep deadlock.
// Reverting the migration that drops organization_directory, or the one
// that alters its cascading foreign key into blobfs_directory, locks the
// two tables in the opposite order from a sweep pass's directory removal,
// whose delete cascades from blobfs_directory into organization_directory,
// so Postgres aborts one of them (SQLSTATE 40P01). The gate is per
// process: it keeps this process's sweep out of this process's schema
// change, and nothing else. A reset is a development operation; with
// several replicas, another replica's sweep, or API traffic that removes a
// document root, can still meet it, and a multi-replica reset would need
// a database lock the sweep takes too.
type SchemaGate interface {
	Exclusive(ctx context.Context) (release func(), err error)
}

type handler struct {
	service *admin.Service
	gate    SchemaGate
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
//     answers with what it stored by seed contribution: the rows it inserted,
//     and the files it wrote once they committed
//   - state: resets the database to the state its body names, once the
//     body confirms it, and answers with the transition
//
// up, down, steps, and state change the schema, so each holds gate
// exclusively around its operation: the process's sweep finishes its pass
// in flight first and starts no other until the change is done. verify
// and force hold nothing: verify reads, and force sets a set's history
// without running any file, so neither takes a lock the sweep contends
// for; seed only inserts rows and writes files.
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
func Routes(service *admin.Service, gate SchemaGate) *web.Group {
	h := &handler{service: service, gate: gate}
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
// rejected verb argument, a set the migrator does not declare, a version
// outside the set, a state the seeder does not declare, and an unconfirmed
// reset are 400; a seed the environment cannot serve is 403. The schema
// states an operation cannot proceed from are conflicts (409): dirty,
// pending, a history the set does not carry, a migration with no down, a
// set above that has applied migrations, and a set below with pending
// ones. A dialect without the lock capability stays unmatched: it is a
// wiring defect (500).
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
