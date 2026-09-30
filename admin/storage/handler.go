package storage

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
)

// Store is what the group reads and triggers on the object store: a
// *storage.Store, started or not.
type Store interface {
	Ready() bool
	Container() string
	Capabilities() storage.Capabilities
	EnsureContainer(ctx context.Context) error
}

// Diagnostics is the store's state as an operator reads it: whether a live
// probe succeeds, the container the service is configured for, and the
// longest key the provider accepts.
type Diagnostics struct {
	Ready        bool   `json:"ready"`
	Container    string `json:"container"`
	MaxKeyLength int    `json:"max_key_length"`
}

type handler struct {
	store Store
}

// Routes builds the object storage admin domain's route group, rooted at
// /storage:
//
//   - GET /diagnostics reads the store's diagnostics; Ready runs a live
//     probe, so a store that stopped answering reads not ready
//   - POST /container creates the configured container, succeeding when it
//     exists, and answers with the diagnostics after it
//
// A store that cannot be reached is a 503 carrying the provider's reason,
// since an operator needs to know why the container could not be made. The
// composition root hands the group the service's logger, which records the
// cause of every 5xx the error writer sends.
func Routes(store Store, logger *slog.Logger) *web.Group {
	h := &handler{store: store}
	ew := web.NewErrorWriter(logger, status)
	ew.Detail(http.StatusServiceUnavailable)
	g := web.NewGroup("/storage")
	g.SetErrorWriter(ew)
	g.HandleErr("GET", "/diagnostics", h.diagnostics)
	g.HandleErr("POST", "/container", h.container)
	return g
}

func (h *handler) diagnostics(w http.ResponseWriter, _ *http.Request) error {
	return web.WriteJSON(w, http.StatusOK, Diagnostics{
		Ready:        h.store.Ready(),
		Container:    h.store.Container(),
		MaxKeyLength: h.store.Capabilities().MaxKeyLength,
	})
}

func (h *handler) container(w http.ResponseWriter, r *http.Request) error {
	if err := h.store.EnsureContainer(r.Context()); err != nil {
		return err
	}
	return h.diagnostics(w, r)
}

// status is the domain's error vocabulary as one web.ProblemMatcher: a
// store that is unavailable or not ready is a temporary outage (503). Any
// other provider error stays unmatched, a server fault.
func status(err error) (web.Problem, bool) {
	switch {
	case errors.Is(err, storage.ErrUnavailable), errors.Is(err, storage.ErrNotReady):
		return web.Problem{Status: http.StatusServiceUnavailable}, true
	}
	return web.Problem{}, false
}
